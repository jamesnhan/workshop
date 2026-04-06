package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleListCards(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	cards, err := a.db.ListCards(project)
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	if cards == nil {
		cards = []db.Card{}
	}
	a.jsonOK(w, cards)
}

func (a *API) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	var card db.Card
	if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if card.Title == "" {
		a.jsonError(w, "title is required", http.StatusBadRequest)
		return
	}
	if card.Column == "" {
		card.Column = "backlog"
	}
	if err := a.db.CreateCard(&card); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, card)
}

func (a *API) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var card db.Card
	if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	card.ID = id
	if err := a.db.UpdateCard(&card); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, card)
}

func (a *API) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Column   string `json:"column"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.db.MoveCard(id, req.Column, req.Position); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"status": "moved"})
}

func (a *API) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := a.db.DeleteCard(id); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleListNotes(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	notes, err := a.db.ListNotes(id)
	if err != nil {
		a.serverErr(w, "list notes", err)
		return
	}
	if notes == nil {
		notes = []db.CardNote{}
	}
	a.jsonOK(w, notes)
}

func (a *API) handleAddNote(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		a.jsonError(w, "text is required", http.StatusBadRequest)
		return
	}
	note, err := a.db.AddNote(id, req.Text)
	if err != nil {
		a.serverErr(w, "add note", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, note)
}

func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.db.ListProjects()
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	if projects == nil {
		projects = []string{}
	}
	a.jsonOK(w, projects)
}
