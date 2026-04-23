package v1

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubChannelHub satisfies the ChannelHubAPI interface for unit tests.
type stubChannelHub struct{}

func (s *stubChannelHub) Publish(string, string, string, string) (*db.ChannelMessageRecord, []string, error) {
	return nil, nil, nil
}
func (s *stubChannelHub) Subscribe(string, string, string) error                       { return nil }
func (s *stubChannelHub) Unsubscribe(string, string) error                             { return nil }
func (s *stubChannelHub) ListChannels(string) ([]db.Channel, error)                    { return nil, nil }
func (s *stubChannelHub) ListMessages(string, int) ([]db.ChannelMessageRecord, error)  { return nil, nil }
func (s *stubChannelHub) RegisterListener(string) (<-chan ChannelDeliveryMessage, func()) {
	ch := make(chan ChannelDeliveryMessage)
	return ch, func() { close(ch) }
}
func (s *stubChannelHub) HasListener(string) bool { return false }
func (s *stubChannelHub) SetMode(string)          {}
func (s *stubChannelHub) Mode() string            { return "native" }

// stubUIHub satisfies the UIHub interface for unit tests.
type stubUIHub struct{}

func (s *stubUIHub) Send(string, map[string]any)                                    {}
func (s *stubUIHub) SendAndWait(string, map[string]any, time.Duration) (UIResponse, error) {
	return UIResponse{}, nil
}
func (s *stubUIHub) Resolve(string, UIResponse) bool { return false }

// stubStatusManager satisfies the StatusManager interface for unit tests.
type stubStatusManager struct{}

func (s *stubStatusManager) Set(string, string, string)   {}
func (s *stubStatusManager) Clear(string)                 {}
func (s *stubStatusManager) Broadcast(string, any)        {}
func (s *stubStatusManager) MarkSeen(string)              {}

// stubRecorder satisfies the Recorder interface for unit tests.
type stubRecorder struct{}

func (s *stubRecorder) Start(string, string, int, int) (int64, error) { return 0, nil }
func (s *stubRecorder) Stop(string) (int64, error)                    { return 0, nil }
func (s *stubRecorder) IsRecording(string) int64                      { return 0 }

func newTestProxyAPI(t *testing.T, proxyURL string) (*API, http.Handler) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := tmux.NewNoBridge()
	database := testhelpers.TempDB(t)
	api := New(logger, bridge, nil, database, &stubRecorder{}, &stubStatusManager{}, &stubUIHub{}, &stubChannelHub{})
	if proxyURL != "" {
		require.NoError(t, api.SetTmuxProxy(proxyURL))
	}
	return api, api.Routes()
}

func TestProxy_SessionsProxiedToDesktop(t *testing.T) {
	// Mock desktop Workshop
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]string{
			{"name": "main", "windows": "3"},
		})
	}))
	defer desktop.Close()

	_, handler := newTestProxyAPI(t, desktop.URL)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp []map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	assert.Equal(t, "main", resp[0]["name"])
}

func TestProxy_PanesProxiedToDesktop(t *testing.T) {
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]string{
			{"target": "main:1.1"},
		})
	}))
	defer desktop.Close()

	_, handler := newTestProxyAPI(t, desktop.URL)

	req := httptest.NewRequest("GET", "/panes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxy_KanbanNotProxied(t *testing.T) {
	// Desktop should NOT receive this request
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("kanban request should not be proxied")
	}))
	defer desktop.Close()

	_, handler := newTestProxyAPI(t, desktop.URL)

	req := httptest.NewRequest("GET", "/cards", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxy_HealthReportsProxy(t *testing.T) {
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("health should not be proxied")
	}))
	defer desktop.Close()

	_, handler := newTestProxyAPI(t, desktop.URL)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["headless"])
	assert.NotEmpty(t, resp["tmuxProxy"])

	features := resp["features"].(map[string]any)
	assert.Equal(t, true, features["tmux"])
}

func TestProxy_NoProxy_Returns503(t *testing.T) {
	// No proxy configured — should 503 on tmux routes
	_, handler := newTestProxyAPI(t, "")

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestProxy_PreservesPathAndQuery(t *testing.T) {
	var receivedPath, receivedQuery string
	desktop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"output": "hello"})
	}))
	defer desktop.Close()

	_, handler := newTestProxyAPI(t, desktop.URL)

	req := httptest.NewRequest("GET", "/sessions/main/capture?lines=100&target=main:1.2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/api/v1/sessions/main/capture", receivedPath)
	assert.Contains(t, receivedQuery, "lines=100")
	assert.Contains(t, receivedQuery, "target=main:1.2")
}
