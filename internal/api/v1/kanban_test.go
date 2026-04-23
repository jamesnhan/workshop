package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCards(t *testing.T, api *API, project string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		c := &db.Card{Title: "card", Column: "backlog", Project: project}
		require.NoError(t, api.db.CreateCard(c))
	}
}

func TestHandleListCards_unpaginated(t *testing.T) {
	api := newDBAPI(t)
	seedCards(t, api, "p", 5)

	req := httptest.NewRequest(http.MethodGet, "/cards?project=p", nil)
	w := httptest.NewRecorder()
	api.handleListCards(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Without limit param, response is a plain array.
	var cards []db.Card
	require.NoError(t, json.NewDecoder(w.Body).Decode(&cards))
	assert.Len(t, cards, 5)
}

func TestHandleListCards_paginated(t *testing.T) {
	api := newDBAPI(t)
	seedCards(t, api, "p", 10)

	req := httptest.NewRequest(http.MethodGet, "/cards?project=p&limit=3&offset=2", nil)
	w := httptest.NewRecorder()
	api.handleListCards(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Cards  []db.Card `json:"cards"`
		Total  int       `json:"total"`
		Limit  int       `json:"limit"`
		Offset int       `json:"offset"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 10, resp.Total)
	assert.Equal(t, 3, resp.Limit)
	assert.Equal(t, 2, resp.Offset)
	assert.Len(t, resp.Cards, 3)
}

func TestHandleListCards_paginatedPastEnd(t *testing.T) {
	api := newDBAPI(t)
	seedCards(t, api, "p", 5)

	req := httptest.NewRequest(http.MethodGet, "/cards?project=p&limit=10&offset=3", nil)
	w := httptest.NewRecorder()
	api.handleListCards(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Cards []db.Card `json:"cards"`
		Total int       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Cards, 2)
}

// --- CreateCard ---

func TestHandleCreateCard_happyPath(t *testing.T) {
	api := newDBAPI(t)

	body := `{"title":"new card","project":"proj","column":"backlog","card_type":"feature","priority":"P1"}`
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleCreateCard(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var card db.Card
	require.NoError(t, json.NewDecoder(w.Body).Decode(&card))
	assert.Equal(t, "new card", card.Title)
	assert.Equal(t, "proj", card.Project)
	assert.Equal(t, "backlog", card.Column)
	assert.NotZero(t, card.ID)
}

func TestHandleCreateCard_defaultColumn(t *testing.T) {
	api := newDBAPI(t)

	body := `{"title":"no column"}`
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleCreateCard(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var card db.Card
	require.NoError(t, json.NewDecoder(w.Body).Decode(&card))
	assert.Equal(t, "backlog", card.Column)
}

func TestHandleCreateCard_missingTitle(t *testing.T) {
	api := newDBAPI(t)

	body := `{"project":"proj"}`
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleCreateCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "title is required")
}

func TestHandleCreateCard_invalidJSON(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleCreateCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- GetCard ---

func TestHandleGetCard_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "test", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodGet, "/cards/1", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleGetCard(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var got db.Card
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "test", got.Title)
}

func TestHandleGetCard_notFound(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/999", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	api.handleGetCard(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGetCard_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/abc", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleGetCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- UpdateCard ---

func TestHandleUpdateCard_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "original", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"title":"updated","column":"backlog","project":"p"}`
	req := httptest.NewRequest(http.MethodPut, "/cards/1", strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleUpdateCard(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var got db.Card
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "updated", got.Title)
}

func TestHandleUpdateCard_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPut, "/cards/abc", strings.NewReader(`{"title":"x"}`))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleUpdateCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateCard_invalidJSON(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodPut, "/cards/1", strings.NewReader("{bad"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleUpdateCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- DeleteCard ---

func TestHandleDeleteCard_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "doomed", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodDelete, "/cards/1", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleDeleteCard(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify it's gone
	_, err := api.db.GetCard(card.ID)
	assert.Error(t, err)
}

func TestHandleDeleteCard_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodDelete, "/cards/abc", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleDeleteCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- MoveCard ---

func TestHandleMoveCard_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "moveme", Column: "backlog", Project: "p", Description: "has description"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"column":"in_progress","position":0}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/move", strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleMoveCard(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "moved")
}

func TestHandleMoveCard_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/cards/abc/move", strings.NewReader(`{"column":"done"}`))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleMoveCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleMoveCard_invalidJSON(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodPost, "/cards/1/move", strings.NewReader("{bad"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleMoveCard(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- AddNote ---

func TestHandleAddNote_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"text":"a note"}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/notes", strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleAddNote(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var note db.CardNote
	require.NoError(t, json.NewDecoder(w.Body).Decode(&note))
	assert.Equal(t, "a note", note.Text)
	assert.NotZero(t, note.ID)
}

func TestHandleAddNote_emptyText(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"text":""}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/notes", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleAddNote(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "text is required")
}

func TestHandleAddNote_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/cards/abc/notes", strings.NewReader(`{"text":"x"}`))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleAddNote(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ListNotes ---

func TestHandleListNotes_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))
	_, err := api.db.AddNote(card.ID, "note 1")
	require.NoError(t, err)
	_, err = api.db.AddNote(card.ID, "note 2")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cards/1/notes", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListNotes(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var notes []db.CardNote
	require.NoError(t, json.NewDecoder(w.Body).Decode(&notes))
	assert.Len(t, notes, 2)
}

func TestHandleListNotes_empty(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodGet, "/cards/1/notes", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListNotes(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var notes []db.CardNote
	require.NoError(t, json.NewDecoder(w.Body).Decode(&notes))
	assert.NotNil(t, notes)
	assert.Empty(t, notes)
}

func TestHandleListNotes_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/abc/notes", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleListNotes(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- AddMessage ---

func TestHandleAddMessage_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"author":"agent-1","text":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/messages", strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleAddMessage(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var msg db.CardMessage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&msg))
	assert.Equal(t, "hello", msg.Text)
	assert.Equal(t, "agent-1", msg.Author)
}

func TestHandleAddMessage_emptyText(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{"author":"a","text":""}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/messages", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleAddMessage(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAddMessage_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/cards/abc/messages", strings.NewReader(`{"text":"x"}`))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleAddMessage(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ListMessages ---

func TestHandleListMessages_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))
	_, err := api.db.AddMessage(card.ID, "agent", "msg1")
	require.NoError(t, err)
	_, err = api.db.AddMessage(card.ID, "agent", "msg2")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cards/1/messages", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var msgs []db.CardMessage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&msgs))
	assert.Len(t, msgs, 2)
}

func TestHandleListMessages_empty(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodGet, "/cards/1/messages", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListMessages(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var msgs []db.CardMessage
	require.NoError(t, json.NewDecoder(w.Body).Decode(&msgs))
	assert.NotNil(t, msgs)
	assert.Empty(t, msgs)
}

func TestHandleListMessages_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/abc/messages", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleListMessages(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ListProjects ---

func TestHandleListProjects_empty(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	w := httptest.NewRecorder()
	api.handleListProjects(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var projects []string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&projects))
	assert.NotNil(t, projects)
	assert.Empty(t, projects)
}

func TestHandleListProjects_withCards(t *testing.T) {
	api := newDBAPI(t)

	c1 := &db.Card{Title: "a", Column: "backlog", Project: "alpha"}
	c2 := &db.Card{Title: "b", Column: "backlog", Project: "beta"}
	require.NoError(t, api.db.CreateCard(c1))
	require.NoError(t, api.db.CreateCard(c2))

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	w := httptest.NewRecorder()
	api.handleListProjects(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var projects []string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&projects))
	assert.Len(t, projects, 2)
	assert.Contains(t, projects, "alpha")
	assert.Contains(t, projects, "beta")
}

// --- ListCardLog ---

func TestHandleListCardLog_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p", Description: "has description"}
	require.NoError(t, api.db.CreateCard(card))
	// Move the card to generate a log entry
	require.NoError(t, api.db.MoveCard(card.ID, "in_progress", 0))

	req := httptest.NewRequest(http.MethodGet, "/cards/1/log", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListCardLog(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var entries []db.CardLogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entries))
	assert.NotEmpty(t, entries)
}

func TestHandleListCardLog_emptyLog(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	req := httptest.NewRequest(http.MethodGet, "/cards/1/log", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", card.ID))
	w := httptest.NewRecorder()
	api.handleListCardLog(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var entries []db.CardLogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entries))
	assert.NotNil(t, entries)
}

func TestHandleListCardLog_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/abc/log", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleListCardLog(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ListProjectLog ---

func TestHandleListProjectLog_happyPath(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "myproj", Description: "has description"}
	require.NoError(t, api.db.CreateCard(card))
	require.NoError(t, api.db.MoveCard(card.ID, "in_progress", 0))

	req := httptest.NewRequest(http.MethodGet, "/cards/log?project=myproj", nil)
	w := httptest.NewRecorder()
	api.handleListProjectLog(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var entries []db.CardLogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entries))
	assert.NotEmpty(t, entries)
}

func TestHandleListProjectLog_empty(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/cards/log?project=nope", nil)
	w := httptest.NewRecorder()
	api.handleListProjectLog(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var entries []db.CardLogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entries))
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestHandleListProjectLog_customLimit(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p", Description: "has description"}
	require.NoError(t, api.db.CreateCard(card))
	// Generate multiple log entries
	require.NoError(t, api.db.MoveCard(card.ID, "in_progress", 0))
	require.NoError(t, api.db.MoveCard(card.ID, "review", 0))

	req := httptest.NewRequest(http.MethodGet, "/cards/log?project=p&limit=1", nil)
	w := httptest.NewRecorder()
	api.handleListProjectLog(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var entries []db.CardLogEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entries))
	assert.Len(t, entries, 1)
}

// --- Dependencies ---

func TestHandleAddDependency_happyPath(t *testing.T) {
	api := newDBAPI(t)

	blocker := &db.Card{Title: "blocker", Column: "backlog", Project: "p"}
	blocked := &db.Card{Title: "blocked", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(blocker))
	require.NoError(t, api.db.CreateCard(blocked))

	body := fmt.Sprintf(`{"blockerId":%d}`, blocker.ID)
	req := httptest.NewRequest(http.MethodPost, "/cards/2/blocks", strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", blocked.ID))
	w := httptest.NewRecorder()
	api.handleAddDependency(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(blocker.ID), resp["blockerId"])
	assert.Equal(t, float64(blocked.ID), resp["blockedId"])
}

func TestHandleAddDependency_missingBlockerID(t *testing.T) {
	api := newDBAPI(t)

	card := &db.Card{Title: "t", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(card))

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/cards/1/blocks", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleAddDependency(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "blockerId is required")
}

func TestHandleAddDependency_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/cards/abc/blocks", strings.NewReader(`{"blockerId":1}`))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleAddDependency(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleListDependencies_empty(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/card-dependencies?project=p", nil)
	w := httptest.NewRecorder()
	api.handleListDependencies(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var deps []db.CardDependency
	require.NoError(t, json.NewDecoder(w.Body).Decode(&deps))
	assert.NotNil(t, deps)
	assert.Empty(t, deps)
}

func TestHandleListDependencies_withDeps(t *testing.T) {
	api := newDBAPI(t)

	c1 := &db.Card{Title: "a", Column: "backlog", Project: "p"}
	c2 := &db.Card{Title: "b", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(c1))
	require.NoError(t, api.db.CreateCard(c2))
	require.NoError(t, api.db.AddDependency(c1.ID, c2.ID))

	req := httptest.NewRequest(http.MethodGet, "/card-dependencies?project=p", nil)
	w := httptest.NewRecorder()
	api.handleListDependencies(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var deps []db.CardDependency
	require.NoError(t, json.NewDecoder(w.Body).Decode(&deps))
	assert.Len(t, deps, 1)
}

func TestHandleRemoveDependency_happyPath(t *testing.T) {
	api := newDBAPI(t)

	c1 := &db.Card{Title: "a", Column: "backlog", Project: "p"}
	c2 := &db.Card{Title: "b", Column: "backlog", Project: "p"}
	require.NoError(t, api.db.CreateCard(c1))
	require.NoError(t, api.db.CreateCard(c2))
	require.NoError(t, api.db.AddDependency(c1.ID, c2.ID))

	req := httptest.NewRequest(http.MethodDelete, "/cards/2/blocks/1", nil)
	req.SetPathValue("id", fmt.Sprintf("%d", c2.ID))
	req.SetPathValue("blockerId", fmt.Sprintf("%d", c1.ID))
	w := httptest.NewRecorder()
	api.handleRemoveDependency(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleRemoveDependency_invalidID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodDelete, "/cards/abc/blocks/1", nil)
	req.SetPathValue("id", "abc")
	req.SetPathValue("blockerId", "1")
	w := httptest.NewRecorder()
	api.handleRemoveDependency(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRemoveDependency_invalidBlockerID(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodDelete, "/cards/1/blocks/abc", nil)
	req.SetPathValue("id", "1")
	req.SetPathValue("blockerId", "abc")
	w := httptest.NewRecorder()
	api.handleRemoveDependency(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
