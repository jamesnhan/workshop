package v1

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type publishReq struct {
	Channel string `json:"channel"`
	From    string `json:"from"`
	Body    string `json:"body"`
	Project string `json:"project,omitempty"`
}

type subscribeReq struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Project string `json:"project,omitempty"`
}

func (a *API) handleChannelPublish(w http.ResponseWriter, r *http.Request) {
	var req publishReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	msg, delivered, err := a.channels.Publish(req.Channel, req.From, req.Body, req.Project)
	if err != nil {
		a.serverErr(w, "publish failed", err)
		return
	}
	a.jsonOK(w, map[string]any{
		"message":   msg,
		"delivered": delivered,
	})
}

func (a *API) handleChannelSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := a.channels.Subscribe(req.Channel, req.Target, req.Project); err != nil {
		a.serverErr(w, "subscribe failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"status": "subscribed"})
}

func (a *API) handleChannelUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := a.channels.Unsubscribe(req.Channel, req.Target); err != nil {
		a.serverErr(w, "unsubscribe failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleChannelList(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	channels, err := a.channels.ListChannels(project)
	if err != nil {
		a.serverErr(w, "list failed", err)
		return
	}
	a.jsonOK(w, channels)
}

func (a *API) handleChannelMessages(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	msgs, err := a.channels.ListMessages(name, limit)
	if err != nil {
		a.serverErr(w, "list messages failed", err)
		return
	}
	a.jsonOK(w, msgs)
}

// handleChannelListen is a long-poll endpoint used by Workshop MCP subprocesses
// running inside Claude Code instances. The MCP subprocess registers itself
// for its pane target on startup and reads messages from the response stream
// (one JSON object per line). When a message arrives, the MCP subprocess
// emits a notifications/claude/channel notification to its parent Claude Code.
//
// Connection model: SSE-style streaming. Server keeps the connection open and
// writes one JSON line per message. Client reconnects on disconnect.
func (a *API) handleChannelListen(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("target")
	if target == "" {
		a.jsonError(w, "target is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		a.jsonError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	listener, unregister := a.channels.RegisterListener(target)
	defer unregister()

	// Send an initial heartbeat so the client knows registration succeeded.
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]string{"type": "ready", "target": target})
	flusher.Flush()

	ctx := r.Context()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := enc.Encode(map[string]string{"type": "heartbeat"}); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-listener:
			if !ok {
				return
			}
			payload := map[string]any{
				"type":      "message",
				"id":        msg.ID,
				"channel":   msg.Channel,
				"from":      msg.From,
				"body":      msg.Body,
				"project":   msg.Project,
				"timestamp": msg.Timestamp,
			}
			if err := enc.Encode(payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleChannelMode gets or sets the channel delivery mode.
// GET returns the current mode; PUT changes it.
func (a *API) handleChannelMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.jsonOK(w, map[string]string{"mode": a.channels.Mode()})
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Mode != "auto" && req.Mode != "compat" && req.Mode != "native" {
		a.jsonError(w, "mode must be auto, compat, or native", http.StatusBadRequest)
		return
	}
	a.channels.SetMode(req.Mode)
	a.jsonOK(w, map[string]string{"mode": req.Mode})
}
