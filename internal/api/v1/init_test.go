package v1

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleInit_HeadlessEmpty(t *testing.T) {
	// Headless (NoBridge, no proxy) — sessions/panes empty, projects from DB.
	api := newDBAPI(t)
	api.tmux = tmux.NewNoBridge()

	req := httptest.NewRequest(http.MethodGet, "/init", nil)
	w := httptest.NewRecorder()
	api.handleInit(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Sessions []tmux.Session `json:"sessions"`
		Panes    []tmux.Pane    `json:"panes"`
		Projects []string       `json:"projects"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Should return empty arrays, not null.
	assert.NotNil(t, resp.Sessions)
	assert.NotNil(t, resp.Panes)
	assert.NotNil(t, resp.Projects)
	assert.Empty(t, resp.Sessions)
	assert.Empty(t, resp.Panes)
}

func TestHandleInit_WithProjects(t *testing.T) {
	api := newDBAPI(t)
	api.tmux = tmux.NewNoBridge()

	// Seed a card to create a project.
	card := &db.Card{Title: "test", Column: "backlog", Project: "myproj"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodGet, "/init", nil)
	w := httptest.NewRecorder()
	api.handleInit(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Projects []string `json:"projects"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Projects, "myproj")
}

func TestHandleInit_ViaRoutes(t *testing.T) {
	// Ensure the init endpoint is properly wired up in the router.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := tmux.NewNoBridge()
	database := testhelpers.TempDB(t)
	api := New(logger, bridge, nil, database, &stubRecorder{}, &stubStatusManager{}, &stubUIHub{}, &stubChannelHub{})
	handler := api.Routes()

	req := httptest.NewRequest(http.MethodGet, "/init", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
