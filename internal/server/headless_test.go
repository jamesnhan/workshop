package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1 "github.com/jamesnhan/workshop/internal/api/v1"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHeadlessAPI creates an API wired with NoBridge for headless testing.
func newHeadlessAPI(t *testing.T) (*apiv1.API, http.Handler) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := tmux.NewNoBridge()
	database := testhelpers.TempDB(t)
	outputBuffer := NewOutputBuffer(100)
	statusStore := NewStatusStore()
	recorder := NewRecordingManager(logger, database)
	uiHub := NewUICommandHub(statusStore)
	fake := &fakeDelivery{failFor: map[string]error{}}
	channelHub := NewChannelHub(database, logger, fake, DeliveryNative)
	api := apiv1.New(logger, bridge, outputBuffer, database, recorder, statusStore, &uiHubAdapter{hub: uiHub}, &channelHubAdapter{hub: channelHub})
	return api, api.Routes()
}

func TestHeadless_HealthEndpoint(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, true, resp["headless"])

	features, ok := resp["features"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, features["tmux"])
	assert.Equal(t, true, features["kanban"])
}

func TestHeadless_SessionEndpoint_Returns503(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "headless")
}

func TestHeadless_PanesEndpoint_Returns503(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("GET", "/panes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHeadless_AgentLaunch_Returns503(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("POST", "/agents/launch", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHeadless_KanbanEndpoint_Works(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("GET", "/cards", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHeadless_ActivityEndpoint_Works(t *testing.T) {
	_, handler := newHeadlessAPI(t)

	req := httptest.NewRequest("GET", "/activity", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
