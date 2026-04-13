package server

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes ---

// fakeDelivery captures deliveries for assertion and can be configured to
// fail a specific target.
type fakeDelivery struct {
	mu      sync.Mutex
	calls   []fakeDeliveryCall
	failFor map[string]error
}

type fakeDeliveryCall struct {
	Target string
	Msg    ChannelMessage
}

func (f *fakeDelivery) Name() string { return "fake-compat" }

func (f *fakeDelivery) Deliver(target string, msg ChannelMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[target]; err != nil {
		return err
	}
	f.calls = append(f.calls, fakeDeliveryCall{Target: target, Msg: msg})
	return nil
}

func (f *fakeDelivery) Calls() []fakeDeliveryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeDeliveryCall(nil), f.calls...)
}

// newTestChannelHub wires a hub with a temp DB and fake delivery in the
// given mode. Returns everything the tests need to drive and assert.
func newTestChannelHub(t *testing.T, mode DeliveryMode) (*ChannelHub, *fakeDelivery) {
	t.Helper()
	d := testhelpers.TempDB(t)
	fake := &fakeDelivery{failFor: map[string]error{}}
	hub := NewChannelHub(d, slog.New(slog.NewTextHandler(io.Discard, nil)), fake, mode)
	return hub, fake
}

// --- Subscribe / Unsubscribe ---

func TestSubscribe_rejectsEmptyChannel(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	err := hub.Subscribe("", "workshop:1.1", "p")
	assert.ErrorIs(t, err, ErrEmptyChannelOrTarget)
}

func TestSubscribe_rejectsEmptyTarget(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	err := hub.Subscribe("foo", "", "p")
	assert.ErrorIs(t, err, ErrEmptyChannelOrTarget)
}

func TestSubscribe_idempotent(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("foo", "workshop:1.1", "p"))
	require.NoError(t, hub.Subscribe("foo", "workshop:1.1", "p"), "duplicate subscribe should be silently ignored")
}

func TestUnsubscribe_removesDelivery(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("foo", "workshop:1.1", "p"))
	require.NoError(t, hub.Unsubscribe("foo", "workshop:1.1"))

	_, _, err := hub.Publish("foo", "sender", "hi", "p")
	require.NoError(t, err)
	assert.Empty(t, fake.Calls(), "unsubscribed target should not receive delivery")
}

// --- Publish validation ---

func TestPublish_rejectsEmptyChannel(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	_, _, err := hub.Publish("", "sender", "body", "p")
	assert.ErrorIs(t, err, ErrEmptyChannelOrBody)
}

func TestPublish_rejectsEmptyBody(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	_, _, err := hub.Publish("foo", "sender", "", "p")
	assert.ErrorIs(t, err, ErrEmptyChannelOrBody)
}

// --- Fan-out via compat delivery ---

func TestPublish_fansOutToAllSubscribers(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("standup", "alpha:1.1", "p"))
	require.NoError(t, hub.Subscribe("standup", "beta:1.1", "p"))
	require.NoError(t, hub.Subscribe("standup", "gamma:1.1", "p"))

	stored, delivered, err := hub.Publish("standup", "sender", "good morning", "p")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Len(t, delivered, 3)

	calls := fake.Calls()
	require.Len(t, calls, 3)
	targets := map[string]bool{}
	for _, c := range calls {
		targets[c.Target] = true
		assert.Equal(t, "good morning", c.Msg.Body)
		assert.Equal(t, "standup", c.Msg.Channel)
		assert.Equal(t, "sender", c.Msg.From)
	}
	assert.True(t, targets["alpha:1.1"])
	assert.True(t, targets["beta:1.1"])
	assert.True(t, targets["gamma:1.1"])
}

func TestPublish_doesNotEchoBackToSender(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("room", "alice", "p"))
	require.NoError(t, hub.Subscribe("room", "bob", "p"))

	_, delivered, err := hub.Publish("room", "alice", "hi", "p")
	require.NoError(t, err)
	assert.Equal(t, []string{"bob"}, delivered)

	calls := fake.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "bob", calls[0].Target)
}

func TestPublish_persistsMessageHistory(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("notes", "alpha", "p"))

	_, _, err := hub.Publish("notes", "sender", "first", "p")
	require.NoError(t, err)
	_, _, err = hub.Publish("notes", "sender", "second", "p")
	require.NoError(t, err)

	msgs, err := hub.ListMessages("notes", 10)
	require.NoError(t, err)
	// History is ordered most-recent first by the db layer.
	require.Len(t, msgs, 2)
	bodies := []string{msgs[0].Body, msgs[1].Body}
	assert.Contains(t, bodies, "first")
	assert.Contains(t, bodies, "second")
}

func TestPublish_skipsDeliveryFailuresButContinues(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryCompat)
	fake.failFor["bad:1.1"] = errors.New("nope")
	require.NoError(t, hub.Subscribe("foo", "good:1.1", "p"))
	require.NoError(t, hub.Subscribe("foo", "bad:1.1", "p"))
	require.NoError(t, hub.Subscribe("foo", "also-good:1.1", "p"))

	_, delivered, err := hub.Publish("foo", "sender", "hi", "p")
	require.NoError(t, err)
	// Only the two successful targets.
	assert.ElementsMatch(t, []string{"good:1.1", "also-good:1.1"}, delivered)
}

func TestPublish_zeroSubscribersStillPersists(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryCompat)
	stored, delivered, err := hub.Publish("empty", "sender", "to the void", "p")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Empty(t, delivered)
	assert.Empty(t, fake.Calls())

	// History still has the message.
	msgs, err := hub.ListMessages("empty", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "to the void", msgs[0].Body)
}

// --- Native delivery ---

func TestPublish_nativeModeUsesListener(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryNative)
	require.NoError(t, hub.Subscribe("room", "native:1.1", "p"))
	ch, unregister := hub.RegisterListener("native:1.1")
	defer unregister()

	_, delivered, err := hub.Publish("room", "sender", "hello native", "p")
	require.NoError(t, err)
	assert.Equal(t, []string{"native:1.1"}, delivered)
	assert.Empty(t, fake.Calls(), "compat should NOT have been called in native mode with a listener")

	select {
	case msg := <-ch:
		assert.Equal(t, "hello native", msg.Body)
		assert.Equal(t, "room", msg.Channel)
	case <-time.After(time.Second):
		t.Fatal("native listener did not receive message")
	}
}

func TestPublish_autoModeFallsBackToCompat(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryAuto)
	require.NoError(t, hub.Subscribe("room", "target:1.1", "p"))
	// No listener registered — auto should fall back to compat.

	_, delivered, err := hub.Publish("room", "sender", "hi", "p")
	require.NoError(t, err)
	assert.Equal(t, []string{"target:1.1"}, delivered)
	require.Len(t, fake.Calls(), 1)
	assert.Equal(t, "target:1.1", fake.Calls()[0].Target)
}

func TestPublish_autoModePrefersNativeWhenListenerRegistered(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryAuto)
	require.NoError(t, hub.Subscribe("room", "target:1.1", "p"))
	ch, unregister := hub.RegisterListener("target:1.1")
	defer unregister()

	_, delivered, err := hub.Publish("room", "sender", "hi", "p")
	require.NoError(t, err)
	assert.Equal(t, []string{"target:1.1"}, delivered)
	assert.Empty(t, fake.Calls(), "auto mode with active listener should skip compat")

	select {
	case msg := <-ch:
		assert.Equal(t, "hi", msg.Body)
	case <-time.After(time.Second):
		t.Fatal("native listener did not receive message")
	}
}

func TestPublish_nativeModeNoListenerSkipsTarget(t *testing.T) {
	hub, fake := newTestChannelHub(t, DeliveryNative)
	require.NoError(t, hub.Subscribe("room", "target:1.1", "p"))
	// No listener registered, mode is forced native — should be skipped.

	_, delivered, err := hub.Publish("room", "sender", "hi", "p")
	require.NoError(t, err)
	assert.Empty(t, delivered)
	assert.Empty(t, fake.Calls(), "compat should NOT fire in forced native mode")
}

// --- Listener lifecycle ---

func TestRegisterListener_hasListenerTrue(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryAuto)
	assert.False(t, hub.HasListener("x:1.1"))
	_, unregister := hub.RegisterListener("x:1.1")
	assert.True(t, hub.HasListener("x:1.1"))
	unregister()
	assert.False(t, hub.HasListener("x:1.1"))
}

func TestRegisterListener_replacesExisting(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryAuto)

	old, _ := hub.RegisterListener("x:1.1")
	_, newUnregister := hub.RegisterListener("x:1.1")
	defer newUnregister()

	// The old channel should have been closed by the replacement.
	select {
	case _, ok := <-old:
		assert.False(t, ok, "old listener channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("old listener channel was not closed")
	}

	assert.True(t, hub.HasListener("x:1.1"))
}

// --- Mode toggling ---

func TestSetMode(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	assert.Equal(t, DeliveryCompat, hub.Mode())
	hub.SetMode(DeliveryNative)
	assert.Equal(t, DeliveryNative, hub.Mode())
}

// --- ListChannels ---

func TestListChannels_includesSubscribed(t *testing.T) {
	hub, _ := newTestChannelHub(t, DeliveryCompat)
	require.NoError(t, hub.Subscribe("alpha-chan", "x:1.1", "p"))
	require.NoError(t, hub.Subscribe("beta-chan", "y:1.1", "p"))

	chans, err := hub.ListChannels("p")
	require.NoError(t, err)
	names := make(map[string]bool)
	for _, c := range chans {
		names[c.Name] = true
	}
	assert.True(t, names["alpha-chan"])
	assert.True(t, names["beta-chan"])
}
