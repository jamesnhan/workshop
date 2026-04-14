package v1

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleListCards(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	includeArchived := r.URL.Query().Get("include_archived") == "true"

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	paginated := limitStr != ""

	limit, offset := 0, 0
	if paginated {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	cards, total, err := a.db.ListCardsPaged(project, limit, offset, includeArchived)
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	if cards == nil {
		cards = []db.Card{}
	}

	if paginated {
		a.jsonOK(w, map[string]any{
			"cards":  cards,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	} else {
		a.jsonOK(w, cards)
	}
}

func (a *API) handleGetCard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	card, err := a.db.GetCard(id)
	if err != nil {
		a.jsonError(w, "not found", http.StatusNotFound)
		return
	}
	a.jsonOK(w, card)
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
		if strings.Contains(err.Error(), "not allowed") || strings.Contains(err.Error(), "unknown source column") {
			a.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
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

func (a *API) handleListCardLog(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	entries, err := a.db.ListCardLog(id)
	if err != nil {
		a.serverErr(w, "list card log", err)
		return
	}
	if entries == nil {
		entries = []db.CardLogEntry{}
	}
	a.jsonOK(w, entries)
}

func (a *API) handleListProjectLog(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := a.db.ListProjectLog(project, limit)
	if err != nil {
		a.serverErr(w, "list project log", err)
		return
	}
	if entries == nil {
		entries = []db.CardLogEntry{}
	}
	a.jsonOK(w, entries)
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

// --- Messages ---

func (a *API) handleListMessages(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	msgs, err := a.db.ListMessages(id)
	if err != nil {
		a.serverErr(w, "list messages", err)
		return
	}
	if msgs == nil {
		msgs = []db.CardMessage{}
	}
	a.jsonOK(w, msgs)
}

func (a *API) handleAddMessage(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Author string `json:"author"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		a.jsonError(w, "text is required", http.StatusBadRequest)
		return
	}
	msg, err := a.db.AddMessage(id, req.Author, req.Text)
	if err != nil {
		a.serverErr(w, "add message", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, msg)
}

// --- Dependencies ---

func (a *API) handleListDependencies(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	deps, err := a.db.ListDependencies(project)
	if err != nil {
		a.serverErr(w, "list dependencies", err)
		return
	}
	if deps == nil {
		deps = []db.CardDependency{}
	}
	a.jsonOK(w, deps)
}

func (a *API) handleAddDependency(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	blockedID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		BlockerID int64 `json:"blockerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BlockerID == 0 {
		a.jsonError(w, "blockerId is required", http.StatusBadRequest)
		return
	}
	if err := a.db.AddDependency(req.BlockerID, blockedID); err != nil {
		a.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, map[string]any{"blockerId": req.BlockerID, "blockedId": blockedID})
}

func (a *API) handleRemoveDependency(w http.ResponseWriter, r *http.Request) {
	blockedID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	blockerID, err := strconv.ParseInt(r.PathValue("blockerId"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid blockerId", http.StatusBadRequest)
		return
	}
	if err := a.db.RemoveDependency(blockerID, blockedID); err != nil {
		a.serverErr(w, "remove dependency", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
