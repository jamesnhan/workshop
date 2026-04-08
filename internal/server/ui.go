package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// UICommandHub broadcasts UI commands to the frontend over the existing
// WebSocket channel and (for blocking commands like prompt_user / confirm)
// holds open response channels keyed by correlation ID.
//
// Wire model:
//
//   1. Backend caller invokes Send(action, payload) → returns immediately
//      after broadcasting (fire-and-forget).
//   2. Backend caller invokes SendAndWait(action, payload, timeout) →
//      broadcasts, then blocks until the frontend POSTs to /ui/response/{id}
//      or the timeout elapses.
//   3. Frontend WS message: {type: "ui_command", data: {id, action, payload}}.
//      For blocking actions the frontend sends back a JSON response that the
//      REST handler forwards to Resolve(id, value).
type UICommandHub struct {
	store    *StatusStore
	mu       sync.Mutex
	pending  map[string]chan UIResponse
	nextID   atomic.Uint64
}

// UIResponse is the payload returned by the frontend for a blocking command.
// Cancelled is true if the user dismissed the dialog.
type UIResponse struct {
	Value     any  `json:"value,omitempty"`
	Cancelled bool `json:"cancelled,omitempty"`
}

func NewUICommandHub(store *StatusStore) *UICommandHub {
	return &UICommandHub{
		store:   store,
		pending: make(map[string]chan UIResponse),
	}
}

// Send broadcasts a fire-and-forget UI command. The frontend executes
// it but doesn't return a value.
func (h *UICommandHub) Send(action string, payload map[string]any) {
	h.broadcast("", action, payload)
}

// SendAndWait broadcasts a UI command and blocks until the frontend POSTs
// a response or the timeout expires. Returns ErrUITimeout on timeout.
func (h *UICommandHub) SendAndWait(action string, payload map[string]any, timeout time.Duration) (UIResponse, error) {
	id := h.newID()
	ch := make(chan UIResponse, 1)
	h.mu.Lock()
	h.pending[id] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	h.broadcast(id, action, payload)

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return UIResponse{}, ErrUITimeout
	}
}

// Resolve delivers a frontend response to a pending blocking command.
// Returns false if the ID is unknown (already resolved or timed out).
func (h *UICommandHub) Resolve(id string, resp UIResponse) bool {
	h.mu.Lock()
	ch, ok := h.pending[id]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

func (h *UICommandHub) broadcast(id, action string, payload map[string]any) {
	msg := map[string]any{
		"action":  action,
		"payload": payload,
	}
	if id != "" {
		msg["id"] = id
	}
	h.store.Broadcast("ui_command", msg)
}

func (h *UICommandHub) newID() string {
	n := h.nextID.Add(1)
	return "ui-" + formatUint(n)
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ErrUITimeout indicates a blocking UI command was not answered in time.
var ErrUITimeout = errors.New("ui command timed out")
