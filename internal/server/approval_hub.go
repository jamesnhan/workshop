package server

import (
	"sync"
	"time"
)

// ApprovalHub manages blocking approval requests. Unlike UICommandHub, it
// keys pending channels by the database approval ID (not an ephemeral
// correlation ID), so approvals survive WS reconnects and page reloads.
type ApprovalHub struct {
	store   *StatusStore
	mu      sync.Mutex
	pending map[int64]chan string // approval ID → decision channel
}

func NewApprovalHub(store *StatusStore) *ApprovalHub {
	return &ApprovalHub{
		store:   store,
		pending: make(map[int64]chan string),
	}
}

// WaitForDecision broadcasts the approval request to the frontend and blocks
// until Resolve is called or the timeout elapses. Returns "approved", "denied",
// or "timeout".
func (h *ApprovalHub) WaitForDecision(approvalID int64, payload map[string]any, timeout time.Duration) string {
	ch := make(chan string, 1)
	h.mu.Lock()
	h.pending[approvalID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, approvalID)
		h.mu.Unlock()
	}()

	// Broadcast to all connected WS clients
	h.store.Broadcast("approval_request", payload)

	select {
	case decision := <-ch:
		return decision
	case <-time.After(timeout):
		return "timeout"
	}
}

// Resolve delivers a decision to a pending approval. Returns false if the
// approval ID is unknown (already resolved, timed out, or never existed).
func (h *ApprovalHub) Resolve(approvalID int64, decision string) bool {
	h.mu.Lock()
	ch, ok := h.pending[approvalID]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- decision:
		return true
	default:
		return false
	}
}

// HasPending returns true if there are any unresolved approvals.
func (h *ApprovalHub) HasPending() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending) > 0
}
