package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelAPI(t *testing.T) *API {
	t.Helper()
	api := newDBAPI(t)
	api.channels = &testChannelHub{db: api.db}
	return api
}

// testChannelHub is a real-ish channel hub backed by the DB for testing.
type testChannelHub struct {
	db   *db.DB
	mode string
}

func (h *testChannelHub) Publish(channel, from, body, project string) (*db.ChannelMessageRecord, []string, error) {
	msg, err := h.db.CreateChannelMessage(channel, from, body, project)
	if err != nil {
		return nil, nil, err
	}
	targets, _ := h.db.ListChannelSubscribers(channel)
	return msg, targets, nil
}

func (h *testChannelHub) Subscribe(channel, target, project string) error {
	return h.db.CreateChannelSubscription(channel, target, project)
}

func (h *testChannelHub) Unsubscribe(channel, target string) error {
	return h.db.DeleteChannelSubscription(channel, target)
}

func (h *testChannelHub) ListChannels(project string) ([]db.Channel, error) {
	return h.db.ListChannels(project)
}

func (h *testChannelHub) ListMessages(channel string, limit int) ([]db.ChannelMessageRecord, error) {
	return h.db.ListChannelMessages(channel, limit)
}

func (h *testChannelHub) RegisterListener(string) (<-chan ChannelDeliveryMessage, func()) {
	ch := make(chan ChannelDeliveryMessage)
	return ch, func() { close(ch) }
}

func (h *testChannelHub) HasListener(string) bool { return false }

func (h *testChannelHub) SetMode(mode string) { h.mode = mode }

func (h *testChannelHub) Mode() string {
	if h.mode == "" {
		return "auto"
	}
	return h.mode
}

// --- Publish ---

func TestHandleChannelPublish_happyPath(t *testing.T) {
	api := newChannelAPI(t)

	body := `{"channel":"updates","from":"agent-1","body":"hello world","project":"proj"}`
	req := httptest.NewRequest(http.MethodPost, "/channels/publish", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleChannelPublish(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotNil(t, resp["message"])
}

func TestHandleChannelPublish_invalidJSON(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/channels/publish", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleChannelPublish(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Subscribe ---

func TestHandleChannelSubscribe_happyPath(t *testing.T) {
	api := newChannelAPI(t)

	body := `{"channel":"updates","target":"main:1.1","project":"proj"}`
	req := httptest.NewRequest(http.MethodPost, "/channels/subscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleChannelSubscribe(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "subscribed")
}

func TestHandleChannelSubscribe_invalidJSON(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/channels/subscribe", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleChannelSubscribe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Unsubscribe ---

func TestHandleChannelUnsubscribe_happyPath(t *testing.T) {
	api := newChannelAPI(t)

	// Subscribe first
	require.NoError(t, api.channels.Subscribe("ch", "main:1.1", ""))

	body := `{"channel":"ch","target":"main:1.1"}`
	req := httptest.NewRequest(http.MethodPost, "/channels/unsubscribe", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleChannelUnsubscribe(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleChannelUnsubscribe_invalidJSON(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/channels/unsubscribe", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleChannelUnsubscribe(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- List ---

func TestHandleChannelList_empty(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/channels", nil)
	w := httptest.NewRecorder()
	api.handleChannelList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var channels []db.Channel
	require.NoError(t, json.NewDecoder(w.Body).Decode(&channels))
	// Could be nil or empty depending on DB; just check it doesn't error
}

func TestHandleChannelList_withSubscriptions(t *testing.T) {
	api := newChannelAPI(t)

	require.NoError(t, api.channels.Subscribe("ch1", "main:1.1", "proj"))
	require.NoError(t, api.channels.Subscribe("ch2", "main:1.2", "proj"))

	req := httptest.NewRequest(http.MethodGet, "/channels?project=proj", nil)
	w := httptest.NewRecorder()
	api.handleChannelList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var channels []db.Channel
	require.NoError(t, json.NewDecoder(w.Body).Decode(&channels))
	assert.Len(t, channels, 2)
}

// --- Messages ---

func TestHandleChannelMessages_happyPath(t *testing.T) {
	api := newChannelAPI(t)

	// Publish some messages first
	_, _, err := api.channels.Publish("ch1", "agent", "msg1", "")
	require.NoError(t, err)
	_, _, err = api.channels.Publish("ch1", "agent", "msg2", "")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/channels/ch1/messages", nil)
	req.SetPathValue("name", "ch1")
	w := httptest.NewRecorder()
	api.handleChannelMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var msgs []db.ChannelMessageRecord
	require.NoError(t, json.NewDecoder(w.Body).Decode(&msgs))
	assert.Len(t, msgs, 2)
}

func TestHandleChannelMessages_customLimit(t *testing.T) {
	api := newChannelAPI(t)

	for i := 0; i < 5; i++ {
		_, _, err := api.channels.Publish("ch1", "agent", "msg", "")
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/channels/ch1/messages?limit=2", nil)
	req.SetPathValue("name", "ch1")
	w := httptest.NewRecorder()
	api.handleChannelMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var msgs []db.ChannelMessageRecord
	require.NoError(t, json.NewDecoder(w.Body).Decode(&msgs))
	assert.Len(t, msgs, 2)
}

func TestHandleChannelMessages_empty(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/channels/nonexistent/messages", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	api.handleChannelMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Mode ---

func TestHandleChannelMode_get(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/channel-mode", nil)
	w := httptest.NewRecorder()
	api.handleChannelMode(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "auto", resp["mode"])
}

func TestHandleChannelMode_set(t *testing.T) {
	api := newChannelAPI(t)

	for _, mode := range []string{"auto", "compat", "native"} {
		body := `{"mode":"` + mode + `"}`
		req := httptest.NewRequest(http.MethodPut, "/channel-mode", strings.NewReader(body))
		w := httptest.NewRecorder()
		api.handleChannelMode(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, mode, resp["mode"])
	}
}

func TestHandleChannelMode_invalidMode(t *testing.T) {
	api := newChannelAPI(t)

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest(http.MethodPut, "/channel-mode", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleChannelMode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "mode must be")
}

func TestHandleChannelMode_invalidJSON(t *testing.T) {
	api := newChannelAPI(t)

	req := httptest.NewRequest(http.MethodPut, "/channel-mode", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleChannelMode(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
