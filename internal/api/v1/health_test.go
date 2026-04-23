package v1

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleHealth_LocalMode(t *testing.T) {
	// Local mode: real bridge (NoBridge simulates headless), no proxy.
	api := newDBAPI(t)
	api.tmux = tmux.NewNoBridge()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	api.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, true, resp["headless"])

	features := resp["features"].(map[string]any)
	assert.Equal(t, false, features["tmux"])
	assert.Equal(t, true, features["kanban"])
	assert.Equal(t, false, features["ollama"])
	assert.Equal(t, true, features["channels"])
}

func TestHandleHealth_WithProxy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := tmux.NewNoBridge()
	database := testhelpers.TempDB(t)
	api := New(logger, bridge, nil, database, &stubRecorder{}, &stubStatusManager{}, &stubUIHub{}, &stubChannelHub{})
	require.NoError(t, api.SetTmuxProxy("http://192.168.1.73:9090"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	api.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["headless"])
	assert.NotEmpty(t, resp["tmuxProxy"])

	features := resp["features"].(map[string]any)
	// With proxy, tmux should be available.
	assert.Equal(t, true, features["tmux"])
}

func TestHandleHealth_ContentType(t *testing.T) {
	api := newDBAPI(t)
	api.tmux = tmux.NewNoBridge()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	api.handleHealth(w, req)

	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}
