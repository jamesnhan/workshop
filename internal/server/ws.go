package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/jamesnhan/workshop/internal/telemetry"
	tmuxpkg "github.com/jamesnhan/workshop/internal/tmux"
	"go.opentelemetry.io/otel/attribute"
	"nhooyr.io/websocket"
)

type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type subscribeRequest struct {
	Target string `json:"target"`
}

// paneSession holds a PTY running "tmux attach" for one pane.
type paneSession struct {
	ptmx   *os.File
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

type clientSize struct {
	cols int
	rows int
}

func wsHandler(logger *slog.Logger, bridge tmuxpkg.Bridge, outputBuffer *OutputBuffer, recorder *RecordingManager, statusStore *StatusStore) http.HandlerFunc {
	// Shared across all WS connections: track each client's reported size per pane.
	// Key: pane target, Value: map of client ID → size.
	// Used to resize tmux to the smallest connected client (like native tmux).
	var sizeMu sync.Mutex
	clientSizes := make(map[string]map[string]clientSize) // target → clientID → size
	lastApplied := make(map[string]clientSize)            // target → last dims pushed to tmux

	smallestSize := func(target string) (int, int) {
		sizeMu.Lock()
		defer sizeMu.Unlock()
		clients := clientSizes[target]
		if len(clients) == 0 {
			return 0, 0
		}
		minCols, minRows := 9999, 9999
		for _, sz := range clients {
			if sz.cols < minCols {
				minCols = sz.cols
			}
			if sz.rows < minRows {
				minRows = sz.rows
			}
		}
		return minCols, minRows
	}

	// applyTmuxResize is the single choke-point for tmux ResizeWindow calls.
	// It skips the call (and the SIGWINCH-induced full-screen repaint that
	// gets broadcast to every subscriber) when the dimensions haven't changed
	// since the last applied resize for this target. Without this dedup, every
	// focus/reconnect/force-resize triggered a redundant tmux repaint on every
	// connected client — an amplification loop under multi-client resize (#934).
	applyTmuxResize := func(eb *tmuxpkg.ExecBridge, target string, cols, rows int) bool {
		sizeMu.Lock()
		prev, ok := lastApplied[target]
		if ok && prev.cols == cols && prev.rows == rows {
			sizeMu.Unlock()
			return false
		}
		lastApplied[target] = clientSize{cols: cols, rows: rows}
		sizeMu.Unlock()
		eb.ResizeWindow(target, cols, rows)
		return true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Error("websocket accept failed", "err", err)
			return
		}
		defer conn.CloseNow()

		clientID := fmt.Sprintf("%s-%d", r.RemoteAddr, time.Now().UnixNano())
		logger.Info("websocket connected", "remote", r.RemoteAddr, "clientID", clientID)
		telemetry.WSConnectionsActive.Add(r.Context(), 1)
		defer telemetry.WSConnectionsActive.Add(context.Background(), -1)

		// Clean up this client's sizes on disconnect and re-apply the
		// smallest remaining client's size to each pane (so the desktop
		// resizes back to full size when the phone disconnects).
		defer func() {
			sizeMu.Lock()
			var affectedTargets []string
			for target, clients := range clientSizes {
				delete(clients, clientID)
				if len(clients) == 0 {
					delete(clientSizes, target)
				} else {
					affectedTargets = append(affectedTargets, target)
				}
			}
			sizeMu.Unlock()

			// Re-apply smallest size for panes that still have clients.
			if eb, ok := bridge.(*tmuxpkg.ExecBridge); ok {
				for _, target := range affectedTargets {
					cols, rows := smallestSize(target)
					if cols > 0 && rows > 0 {
						if applyTmuxResize(eb, target, cols, rows) {
							logger.Info("resize-on-disconnect", "target", target, "clientID", clientID, "newCols", cols, "newRows", rows)
						}
					}
				}
			}
		}()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		var mu sync.Mutex
		sessions := make(map[string]*paneSession)

		// Single writer goroutine — all outbound messages go through this
		// channel to avoid concurrent writes on the WebSocket conn.
		outCh := make(chan []byte, 256)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-outCh:
					if !ok {
						return
					}
					if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
						return
					}
					telemetry.WSMessagesTotal.Add(ctx, 1, telemetry.MetricAttrs(attribute.String("direction", "out")))
				}
			}
		}()

		// wsSend enqueues a message for the writer goroutine.
		// Drops the message if the channel is full (backpressure).
		wsSend := func(msg []byte) {
			select {
			case outCh <- msg:
			default:
				// Drop message under backpressure rather than blocking
			}
		}

		// Subscribe to status changes and forward to this WS client
		statusCh := statusStore.Subscribe()
		defer statusStore.Unsubscribe(statusCh)

		// Send current statuses on connect
		for _, ps := range statusStore.GetAll() {
			payload, _ := json.Marshal(ps)
			msg, _ := json.Marshal(wsMessage{Type: "pane_status", Data: payload})
			wsSend(msg)
		}

		// Forward status changes from store to WS
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-statusCh:
					if !ok {
						return
					}
					wsSend(msg)
				}
			}
		}()

		cleanup := func() {
			mu.Lock()
			defer mu.Unlock()
			for target, s := range sessions {
				s.cancel()
				s.ptmx.Close()
				s.cmd.Process.Kill()
				s.cmd.Wait()
				delete(sessions, target)
			}
		}
		defer cleanup()

		// Periodically capture full scrollback for each subscribed pane and
		// replace the search buffer. Uses tmux capture-pane -S - which gives
		// us complete history with clean Unicode/Nerd Font glyphs.
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					mu.Lock()
					targets := make([]string, 0, len(sessions))
					for t := range sessions {
						targets = append(targets, t)
					}
					mu.Unlock()

					for _, t := range targets {
						output, err := bridge.CapturePaneAll(t)
						if err == nil && output != "" {
							outputBuffer.UpdateFromCapture(t, output)
						}
					}
				}
			}
		}()

		sendOutput := func(target string, data []byte) {
			// Search buffer is fed by periodic capture-pane (see goroutine below),
			// not from raw PTY data, to preserve Unicode/Nerd Font glyphs.

			payload, _ := json.Marshal(map[string]string{
				"target": target,
				"data":   string(data),
			})
			msg, _ := json.Marshal(wsMessage{
				Type: "output",
				Data: payload,
			})
			wsSend(msg)
		}

		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				logger.Info("websocket closed", "remote", r.RemoteAddr, "err", err)
				return
			}

			var msg wsMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			telemetry.WSMessagesTotal.Add(ctx, 1, telemetry.MetricAttrs(attribute.String("direction", "in"), attribute.String("kind", msg.Type)))

			switch msg.Type {
			case "subscribe":
				var req subscribeRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" {
					continue
				}
				target := req.Target
				logger.Info("subscribe", "target", target)

				// Clean up existing session for this target
				mu.Lock()
				if old, ok := sessions[target]; ok {
					old.cancel()
					old.ptmx.Close()
					old.cmd.Process.Kill()
					old.cmd.Wait()
					delete(sessions, target)
				}
				mu.Unlock()

				// Start a PTY running tmux attach to this pane's window
				cmd := exec.Command("tmux", "attach-session", "-t", target)
				ptmx, err := pty.Start(cmd)
				if err != nil {
					logger.Error("pty start failed", "target", target, "err", err)
					continue
				}

				subCtx, subCancel := context.WithCancel(ctx)
				sess := &paneSession{ptmx: ptmx, cmd: cmd, cancel: subCancel}

				mu.Lock()
				sessions[target] = sess
				mu.Unlock()

				// Read PTY output and batch into ~16ms flushes to reduce WS message count
				go func(t string, f *os.File, ctx context.Context) {
					buf := make([]byte, 4096)
					var accum []byte
					ticker := time.NewTicker(16 * time.Millisecond)
					defer ticker.Stop()
					readCh := make(chan []byte, 64)

					// PTY reader goroutine — sends chunks to readCh
					go func() {
						defer close(readCh)
						for {
							n, err := f.Read(buf)
							if n > 0 {
								chunk := make([]byte, n)
								copy(chunk, buf[:n])
								select {
								case readCh <- chunk:
								case <-ctx.Done():
									return
								}
							}
							if err != nil {
								return
							}
						}
					}()

					for {
						select {
						case <-ctx.Done():
							return
						case chunk, ok := <-readCh:
							if !ok {
								// PTY closed — flush remaining
								if len(accum) > 0 {
									sendOutput(t, accum)
								}
								return
							}
							accum = append(accum, chunk...)
						case <-ticker.C:
							if len(accum) > 0 {
								sendOutput(t, accum)
								accum = nil
							}
						}
					}
				}(target, ptmx, subCtx)

			case "unsubscribe":
				var req subscribeRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" {
					continue
				}
				mu.Lock()
				if s, ok := sessions[req.Target]; ok {
					s.cancel()
					s.ptmx.Close()
					s.cmd.Process.Kill()
					s.cmd.Wait()
					delete(sessions, req.Target)
				}
				mu.Unlock()
				logger.Info("unsubscribe", "target", req.Target)

				// Remove this client's size entry for the pane and re-apply
				// smallest remaining size. This restores the desktop's full
				// dimensions when the phone switches away from a pane.
				sizeMu.Lock()
				if clients, ok := clientSizes[req.Target]; ok {
					delete(clients, clientID)
					if len(clients) == 0 {
						delete(clientSizes, req.Target)
					}
				}
				sizeMu.Unlock()
				if eb, ok := bridge.(*tmuxpkg.ExecBridge); ok {
					cols, rows := smallestSize(req.Target)
					if cols > 0 && rows > 0 {
						go func(t string, c, r int) {
							if applyTmuxResize(eb, t, c, r) {
								logger.Info("resize-on-unsub", "target", t, "clientID", clientID, "cols", c, "rows", r)
							}
						}(req.Target, cols, rows)
					}
				}

			case "resize":
				var req struct {
					Target string `json:"target"`
					Cols   int    `json:"cols"`
					Rows   int    `json:"rows"`
				}
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" || req.Cols < 1 || req.Rows < 1 {
					continue
				}
				mu.Lock()
				s := sessions[req.Target]
				mu.Unlock()
				// Track this client's size for the pane. Also remove this
				// client's entries for any other panes — on mobile, the user
				// views one pane at a time, so switching panes should release
				// the size constraint on the old pane.
				sizeMu.Lock()
				var staleTargets []string
				for target, clients := range clientSizes {
					if target != req.Target {
						if _, ok := clients[clientID]; ok {
							delete(clients, clientID)
							if len(clients) == 0 {
								delete(clientSizes, target)
							}
							staleTargets = append(staleTargets, target)
						}
					}
				}
				if clientSizes[req.Target] == nil {
					clientSizes[req.Target] = make(map[string]clientSize)
				}
				clientSizes[req.Target][clientID] = clientSize{cols: req.Cols, rows: req.Rows}
				sizeMu.Unlock()

				// Re-apply smallest size to panes we just left.
				if eb, ok := bridge.(*tmuxpkg.ExecBridge); ok {
					for _, target := range staleTargets {
						c, r := smallestSize(target)
						if c > 0 && r > 0 {
							go func(t string, c, r int) {
								if applyTmuxResize(eb, t, c, r) {
									logger.Info("resize-on-pane-switch", "target", t, "clientID", clientID, "cols", c, "rows", r)
								}
							}(target, c, r)
						}
					}
				}

				// Use the smallest connected client's dimensions (like native tmux).
				cols, rows := smallestSize(req.Target)
				if cols < 1 || rows < 1 {
					cols, rows = req.Cols, req.Rows
				}
				if s != nil {
					pty.Setsize(s.ptmx, &pty.Winsize{
						Cols: uint16(cols),
						Rows: uint16(rows),
					})
				}
				if eb, ok := bridge.(*tmuxpkg.ExecBridge); ok {
					go func(t string, c, r int) {
						if applyTmuxResize(eb, t, c, r) {
							logger.Info("resize", "target", t, "clientID", clientID,
								"clientCols", req.Cols, "clientRows", req.Rows,
								"effectiveCols", c, "effectiveRows", r)
						}
					}(req.Target, cols, rows)
				}

			case "input":
				var req struct {
					Target string `json:"target"`
					Data   string `json:"data"`
				}
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" {
					continue
				}
				// Write directly to the PTY master — goes through line discipline
				mu.Lock()
				s := sessions[req.Target]
				mu.Unlock()
				if s != nil {
					s.ptmx.WriteString(req.Data)
				}

			case "record_start":
				var req struct {
					Target string `json:"target"`
					Name   string `json:"name"`
					Cols   int    `json:"cols"`
					Rows   int    `json:"rows"`
				}
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" {
					continue
				}
				id, err := recorder.Start(req.Target, req.Name, req.Cols, req.Rows)
				if err != nil {
					logger.Warn("start recording failed", "err", err)
					continue
				}

				// Send recording ID back to frontend
				respData, _ := json.Marshal(map[string]any{"recordingId": id, "target": req.Target})
				respMsg, _ := json.Marshal(wsMessage{Type: "recording_started", Data: respData})
				wsSend(respMsg)

			case "record_stop":
				var req struct {
					Target string `json:"target"`
				}
				if err := json.Unmarshal(msg.Data, &req); err != nil || req.Target == "" {
					continue
				}
				recorder.Stop(req.Target)
			}
		}
	}
}
