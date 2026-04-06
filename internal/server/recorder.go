package server

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	dbpkg "github.com/jamesnhan/workshop/internal/db"
)

// RecordingManager captures pane output independently of WebSocket connections.
type RecordingManager struct {
	mu        sync.Mutex
	active    map[string]*recSession // target → session
	db        *dbpkg.DB
	logger    *slog.Logger
}

type recSession struct {
	id      int64
	target  string
	startMs int64
	ptmx    *os.File
	cmd     *exec.Cmd
	cancel  context.CancelFunc
}

func NewRecordingManager(logger *slog.Logger, database *dbpkg.DB) *RecordingManager {
	return &RecordingManager{
		active: make(map[string]*recSession),
		db:     database,
		logger: logger,
	}
}

// Start begins recording a pane's output. It opens its own PTY to tmux attach.
func (rm *RecordingManager) Start(target, name string, cols, rows int) (int64, error) {
	rm.mu.Lock()
	// Stop existing recording for this target if any
	if existing, ok := rm.active[target]; ok {
		rm.stopLocked(existing)
	}
	rm.mu.Unlock()

	id, err := rm.db.CreateRecording(name, target, cols, rows)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command("tmux", "attach-session", "-t", target)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		rm.db.StopRecording(id, 0)
		return 0, err
	}

	// Set PTY size to match the pane
	pty.Setsize(ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})

	ctx, cancel := context.WithCancel(context.Background())
	sess := &recSession{
		id:      id,
		target:  target,
		startMs: time.Now().UnixMilli(),
		ptmx:    ptmx,
		cmd:     cmd,
		cancel:  cancel,
	}

	rm.mu.Lock()
	rm.active[target] = sess
	rm.mu.Unlock()

	rm.logger.Info("recording started", "id", id, "target", target)

	// Read PTY output and write frames to DB
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := ptmx.Read(buf)
			if n > 0 {
				offsetMs := int(time.Now().UnixMilli() - sess.startMs)
				rm.db.AppendFrame(id, offsetMs, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	return id, nil
}

// Stop ends the recording for a target and returns the recording ID.
func (rm *RecordingManager) Stop(target string) (int64, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	sess, ok := rm.active[target]
	if !ok {
		return 0, nil
	}
	rm.stopLocked(sess)
	return sess.id, nil
}

// stopLocked stops a recording session. Caller must hold rm.mu.
func (rm *RecordingManager) stopLocked(sess *recSession) {
	sess.cancel()
	sess.ptmx.Close()
	sess.cmd.Process.Kill()
	sess.cmd.Wait()
	delete(rm.active, sess.target)

	durationMs := time.Now().UnixMilli() - sess.startMs
	rm.db.StopRecording(sess.id, durationMs)
	rm.logger.Info("recording stopped", "id", sess.id, "duration_ms", durationMs)
}

// IsRecording returns the recording ID if the target is being recorded, 0 otherwise.
func (rm *RecordingManager) IsRecording(target string) int64 {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if sess, ok := rm.active[target]; ok {
		return sess.id
	}
	return 0
}

// StopAll stops all active recordings. Called on shutdown.
func (rm *RecordingManager) StopAll() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, sess := range rm.active {
		rm.stopLocked(sess)
	}
}
