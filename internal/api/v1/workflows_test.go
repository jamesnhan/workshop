package v1

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDBAPI(t *testing.T) *API {
	t.Helper()
	return &API{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		db:     testhelpers.TempDB(t),
	}
}

func TestGetWorkflow_defaultFallback(t *testing.T) {
	api := newDBAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/workflows?project=unknown", nil)
	w := httptest.NewRecorder()
	api.handleGetWorkflow(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var wf db.WorkflowConfig
	require.NoError(t, json.NewDecoder(w.Body).Decode(&wf))
	assert.Len(t, wf.Columns, 4)
	assert.Equal(t, "backlog", wf.Columns[0].ID)
}

func TestSetWorkflow_roundTrip(t *testing.T) {
	api := newDBAPI(t)

	body := `{"project":"test","config":{"columns":[{"id":"todo","label":"To Do"},{"id":"done","label":"Done"}],"transitions":{"todo":["done"],"done":["todo"]}}}`
	req := httptest.NewRequest(http.MethodPut, "/workflows", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleSetWorkflow(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Read it back
	req2 := httptest.NewRequest(http.MethodGet, "/workflows?project=test", nil)
	w2 := httptest.NewRecorder()
	api.handleGetWorkflow(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var wf db.WorkflowConfig
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&wf))
	assert.Len(t, wf.Columns, 2)
	assert.Equal(t, "todo", wf.Columns[0].ID)
}

func TestSetWorkflow_missingProject(t *testing.T) {
	api := newDBAPI(t)
	body := `{"config":{"columns":[{"id":"a","label":"A"}]}}`
	req := httptest.NewRequest(http.MethodPut, "/workflows", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleSetWorkflow(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetWorkflow_emptyColumns(t *testing.T) {
	api := newDBAPI(t)
	body := `{"project":"p","config":{"columns":[]}}`
	req := httptest.NewRequest(http.MethodPut, "/workflows", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleSetWorkflow(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMoveCard_invalidTransitionReturns400(t *testing.T) {
	api := newDBAPI(t)

	// Create a card in backlog
	card := &db.Card{Title: "test", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	// Try to move backlog → done (invalid in default workflow)
	body := `{"column":"done","position":0}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/move", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleMoveCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "not allowed")
}
