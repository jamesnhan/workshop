package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// PaneStatus represents the status of a pane set by an agent.
type PaneStatus struct {
	Target  string `json:"target"`
	Status  string `json:"status"`  // green, yellow, red
	Message string `json:"message"` // short description
}

// StatusStore holds pane statuses in memory and notifies listeners on changes.
type StatusStore struct {
	mu        sync.RWMutex
	statuses  map[string]PaneStatus // target → status
	listeners []chan []byte
	monitor   *PaneMonitor // for MarkSeen delegation; set after construction
}

// AttachMonitor wires a PaneMonitor so MarkSeen can forward to it.
func (s *StatusStore) AttachMonitor(m *PaneMonitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.monitor = m
}

// MarkSeen delegates to the attached pane monitor.
func (s *StatusStore) MarkSeen(target string) {
	s.mu.RLock()
	m := s.monitor
	s.mu.RUnlock()
	if m != nil {
		m.MarkSeen(target)
	}
}

func NewStatusStore() *StatusStore {
	return &StatusStore{
		statuses: make(map[string]PaneStatus),
	}
}

// Set updates the status for a target and broadcasts to all listeners.
func (s *StatusStore) Set(target, status, message string) {
	ps := PaneStatus{Target: target, Status: status, Message: message}
	s.mu.Lock()
	s.statuses[target] = ps
	listeners := make([]chan []byte, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()

	s.broadcast("pane_status", ps, listeners)
}

// Clear removes the status for a target and broadcasts to all listeners.
// Also removes the hook lock file so the Stop hook can set green again.
func (s *StatusStore) Clear(target string) {
	s.mu.Lock()
	delete(s.statuses, target)
	listeners := make([]chan []byte, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()

	// Remove hook lock file so Stop hook can set green on next completion
	safeName := strings.NewReplacer(":", "-", ".", "-").Replace(target)
	os.Remove("/tmp/workshop-pane-waiting-" + safeName)

	s.broadcast("pane_status_clear", map[string]string{"target": target}, listeners)
}

// GetAll returns all current statuses.
func (s *StatusStore) GetAll() map[string]PaneStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]PaneStatus, len(s.statuses))
	for k, v := range s.statuses {
		result[k] = v
	}
	return result
}

// Subscribe returns a channel that receives status change messages.
func (s *StatusStore) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	s.mu.Lock()
	s.listeners = append(s.listeners, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes a listener channel.
func (s *StatusStore) Unsubscribe(ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.listeners {
		if c == ch {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}

// Broadcast sends an arbitrary message to all connected WS clients.
func (s *StatusStore) Broadcast(msgType string, data any) {
	s.mu.RLock()
	listeners := make([]chan []byte, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.RUnlock()
	s.broadcast(msgType, data, listeners)
}

func (s *StatusStore) broadcast(msgType string, data any, listeners []chan []byte) {
	payload, _ := json.Marshal(data)
	msg, _ := json.Marshal(map[string]json.RawMessage{
		"type": json.RawMessage(`"` + msgType + `"`),
		"data": payload,
	})
	for _, ch := range listeners {
		select {
		case ch <- msg:
		default:
			// Drop if listener is slow
		}
	}
}
