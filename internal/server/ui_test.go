package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHub builds a real StatusStore + UICommandHub pair and subscribes a
// listener so tests can assert on broadcast payloads. Cleanup is automatic.
func newTestHub(t *testing.T) (*UICommandHub, chan []byte) {
	t.Helper()
	store := NewStatusStore()
	ch := store.Subscribe()
	hub := NewUICommandHub(store)
	return hub, ch
}

// decodeUICommand pulls the {type, data} envelope off the wire and returns
// the inner data map. Fails the test if the envelope isn't a ui_command or
// if decoding fails.
func decodeUICommand(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "ui_command", env.Type)
	var data map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &data))
	return data
}

// --- Send (fire-and-forget) ---

func TestSend_broadcastsWithoutID(t *testing.T) {
	hub, ch := newTestHub(t)

	hub.Send("show_toast", map[string]any{"message": "hello", "kind": "info"})

	select {
	case raw := <-ch:
		data := decodeUICommand(t, raw)
		assert.Equal(t, "show_toast", data["action"])
		// Fire-and-forget: no correlation id field.
		assert.NotContains(t, data, "id")
		payload, ok := data["payload"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "hello", payload["message"])
		assert.Equal(t, "info", payload["kind"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

// --- SendAndWait happy path ---

func TestSendAndWait_resolvesOnResponse(t *testing.T) {
	hub, ch := newTestHub(t)

	var (
		wg   sync.WaitGroup
		resp UIResponse
		err  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err = hub.SendAndWait("confirm", map[string]any{"title": "Really?"}, time.Second)
	}()

	// Drain the broadcast to learn the correlation id, then resolve it.
	raw := <-ch
	data := decodeUICommand(t, raw)
	id, ok := data["id"].(string)
	require.True(t, ok, "blocking command must include id")
	assert.Equal(t, "confirm", data["action"])

	ok = hub.Resolve(id, UIResponse{Value: true})
	assert.True(t, ok)

	wg.Wait()
	require.NoError(t, err)
	assert.Equal(t, true, resp.Value)
	assert.False(t, resp.Cancelled)
}

// --- SendAndWait cancel path ---

func TestSendAndWait_cancelled(t *testing.T) {
	hub, ch := newTestHub(t)

	var (
		wg   sync.WaitGroup
		resp UIResponse
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, _ = hub.SendAndWait("prompt_user", map[string]any{"title": "Name?"}, time.Second)
	}()

	raw := <-ch
	id := decodeUICommand(t, raw)["id"].(string)
	hub.Resolve(id, UIResponse{Cancelled: true})

	wg.Wait()
	assert.True(t, resp.Cancelled)
}

// --- SendAndWait timeout ---

func TestSendAndWait_timeout(t *testing.T) {
	hub, ch := newTestHub(t)

	done := make(chan struct{})
	var err error
	go func() {
		_, err = hub.SendAndWait("confirm", nil, 50*time.Millisecond)
		close(done)
	}()

	// Drain the broadcast so the test channel doesn't stall on a full buffer.
	<-ch

	select {
	case <-done:
		assert.ErrorIs(t, err, ErrUITimeout)
	case <-time.After(time.Second):
		t.Fatal("SendAndWait did not timeout")
	}
}

// --- Resolve edge cases ---

func TestResolve_unknownIDReturnsFalse(t *testing.T) {
	hub, _ := newTestHub(t)
	assert.False(t, hub.Resolve("no-such-id", UIResponse{Value: 1}))
}

func TestResolve_afterTimeoutReturnsFalse(t *testing.T) {
	hub, ch := newTestHub(t)

	done := make(chan string, 1)
	go func() {
		_, _ = hub.SendAndWait("confirm", nil, 20*time.Millisecond)
		done <- "done"
	}()

	raw := <-ch
	id := decodeUICommand(t, raw)["id"].(string)
	<-done // wait for the SendAndWait to time out and release the id

	// Resolving after timeout should be a no-op.
	assert.False(t, hub.Resolve(id, UIResponse{Value: "too late"}))
}

// --- Concurrent SendAndWait ---

func TestSendAndWait_concurrentDistinctIDs(t *testing.T) {
	hub, ch := newTestHub(t)

	const n = 10
	responses := make(chan UIResponse, n)
	for i := 0; i < n; i++ {
		go func() {
			resp, _ := hub.SendAndWait("confirm", nil, 2*time.Second)
			responses <- resp
		}()
	}

	// Collect broadcast ids as they come in.
	ids := make([]string, 0, n)
	seen := make(map[string]bool)
	for len(ids) < n {
		raw := <-ch
		id, ok := decodeUICommand(t, raw)["id"].(string)
		require.True(t, ok)
		require.False(t, seen[id], "duplicate id %s across concurrent calls", id)
		seen[id] = true
		ids = append(ids, id)
	}

	// Resolve them all.
	for i, id := range ids {
		hub.Resolve(id, UIResponse{Value: i})
	}

	// Every goroutine should return with its own value.
	gotValues := make(map[int]bool)
	for i := 0; i < n; i++ {
		resp := <-responses
		v, ok := resp.Value.(int)
		require.True(t, ok)
		gotValues[v] = true
	}
	assert.Len(t, gotValues, n, "expected %d distinct values back", n)
}
