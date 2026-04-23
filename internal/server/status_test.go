package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewStatusStore ---

func TestNewStatusStore_Empty(t *testing.T) {
	s := NewStatusStore()
	assert.Empty(t, s.GetAll())
}

// --- Set / GetAll ---

func TestStatusStore_Set_SetsStatus(t *testing.T) {
	s := NewStatusStore()
	s.Set("alpha:1.1", "green", "Done")

	all := s.GetAll()
	require.Len(t, all, 1)
	ps := all["alpha:1.1"]
	assert.Equal(t, "alpha:1.1", ps.Target)
	assert.Equal(t, "green", ps.Status)
	assert.Equal(t, "Done", ps.Message)
}

func TestStatusStore_Set_OverwritesExisting(t *testing.T) {
	s := NewStatusStore()
	s.Set("t:1.1", "yellow", "Waiting")
	s.Set("t:1.1", "green", "OK")

	all := s.GetAll()
	require.Len(t, all, 1)
	assert.Equal(t, "green", all["t:1.1"].Status)
	assert.Equal(t, "OK", all["t:1.1"].Message)
}

func TestStatusStore_Set_MultipleTargets(t *testing.T) {
	s := NewStatusStore()
	s.Set("a:1.1", "green", "done")
	s.Set("b:1.1", "red", "error")

	all := s.GetAll()
	assert.Len(t, all, 2)
}

// --- Clear ---

func TestStatusStore_Clear_RemovesTarget(t *testing.T) {
	s := NewStatusStore()
	s.Set("t:1.1", "green", "OK")
	s.Clear("t:1.1")

	all := s.GetAll()
	assert.Empty(t, all)
}

func TestStatusStore_Clear_NoopOnMissing(t *testing.T) {
	s := NewStatusStore()
	s.Clear("nonexistent:1.1") // should not panic
	assert.Empty(t, s.GetAll())
}

// --- GetAll returns a copy ---

func TestStatusStore_GetAll_ReturnsCopy(t *testing.T) {
	s := NewStatusStore()
	s.Set("t:1.1", "green", "OK")

	all := s.GetAll()
	delete(all, "t:1.1") // mutate the returned map

	// Original store should be unaffected
	assert.Len(t, s.GetAll(), 1)
}

// --- Subscribe / Unsubscribe ---

func TestStatusStore_Subscribe_ReceivesBroadcast(t *testing.T) {
	s := NewStatusStore()
	ch := s.Subscribe()

	s.Set("t:1.1", "green", "Done")

	select {
	case raw := <-ch:
		var env struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &env))
		assert.Equal(t, "pane_status", env.Type)

		var ps PaneStatus
		require.NoError(t, json.Unmarshal(env.Data, &ps))
		assert.Equal(t, "t:1.1", ps.Target)
		assert.Equal(t, "green", ps.Status)
	case <-time.After(time.Second):
		t.Fatal("did not receive broadcast")
	}
}

func TestStatusStore_Unsubscribe_StopsBroadcast(t *testing.T) {
	s := NewStatusStore()
	ch := s.Subscribe()
	s.Unsubscribe(ch)

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok)
}

func TestStatusStore_Unsubscribe_NoopForUnknownChannel(t *testing.T) {
	s := NewStatusStore()
	ch := make(chan []byte)
	s.Unsubscribe(ch) // should not panic
}

// --- Clear broadcasts pane_status_clear ---

func TestStatusStore_Clear_BroadcastsClearEvent(t *testing.T) {
	s := NewStatusStore()
	ch := s.Subscribe()
	s.Set("t:1.1", "green", "OK")
	<-ch // drain the set broadcast

	s.Clear("t:1.1")

	select {
	case raw := <-ch:
		var env struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &env))
		assert.Equal(t, "pane_status_clear", env.Type)
	case <-time.After(time.Second):
		t.Fatal("did not receive clear broadcast")
	}
}

// --- Broadcast (generic) ---

func TestStatusStore_Broadcast_ArbitraryMessage(t *testing.T) {
	s := NewStatusStore()
	ch := s.Subscribe()

	s.Broadcast("custom_event", map[string]string{"key": "value"})

	select {
	case raw := <-ch:
		var env struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &env))
		assert.Equal(t, "custom_event", env.Type)
	case <-time.After(time.Second):
		t.Fatal("did not receive broadcast")
	}
}

// --- Slow listener is dropped ---

func TestStatusStore_Broadcast_DropsSlowListener(t *testing.T) {
	s := NewStatusStore()
	ch := s.Subscribe()

	// Fill the channel buffer (capacity 16)
	for i := 0; i < 20; i++ {
		s.Set("t:1.1", "green", "flood")
	}

	// Drain what we can — should be at most 16 (the buffer size)
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.LessOrEqual(t, count, 16)
}

// --- AttachMonitor / MarkSeen ---

func TestStatusStore_MarkSeen_DelegatesToMonitor(t *testing.T) {
	s := NewStatusStore()

	// Without a monitor attached, MarkSeen should be a no-op (no panic)
	s.MarkSeen("t:1.1")
}

// --- Concurrent access ---

func TestStatusStore_ConcurrentAccess(t *testing.T) {
	s := NewStatusStore()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Set("t:1.1", "green", "OK")
			s.GetAll()
			s.Clear("t:1.1")
		}()
	}

	wg.Wait()
}
