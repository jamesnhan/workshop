package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleListConversations(w http.ResponseWriter, r *http.Request) {
	convs, err := a.db.ListOllamaConversations()
	if err != nil {
		a.serverErr(w, "list conversations", err)
		return
	}
	if convs == nil {
		convs = []db.OllamaConversation{}
	}
	a.jsonOK(w, convs)
}

func (a *API) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var conv db.OllamaConversation
	if err := json.NewDecoder(r.Body).Decode(&conv); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if conv.Title == "" {
		conv.Title = "New Chat"
	}
	if err := a.db.CreateOllamaConversation(&conv); err != nil {
		a.serverErr(w, "create conversation", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, conv)
}

func (a *API) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	conv, err := a.db.GetOllamaConversation(id)
	if err != nil {
		a.jsonError(w, "not found", http.StatusNotFound)
		return
	}
	msgs, err := a.db.ListOllamaMessages(id)
	if err != nil {
		a.serverErr(w, "list messages", err)
		return
	}
	if msgs == nil {
		msgs = []db.OllamaMessage{}
	}
	a.jsonOK(w, map[string]any{
		"conversation": conv,
		"messages":     msgs,
	})
}

func (a *API) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var conv db.OllamaConversation
	if err := json.NewDecoder(r.Body).Decode(&conv); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	conv.ID = id
	if err := a.db.UpdateOllamaConversation(&conv); err != nil {
		a.serverErr(w, "update conversation", err)
		return
	}
	// Return the updated conversation
	updated, err := a.db.GetOllamaConversation(id)
	if err != nil {
		a.serverErr(w, "get updated conversation", err)
		return
	}
	a.jsonOK(w, updated)
}

func (a *API) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := a.db.DeleteOllamaConversation(id); err != nil {
		a.serverErr(w, "delete conversation", err)
		return
	}
	a.jsonOK(w, map[string]string{"status": "deleted"})
}

func (a *API) handleCreateConversationMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Verify conversation exists
	if _, err := a.db.GetOllamaConversation(id); err != nil {
		a.jsonError(w, "conversation not found", http.StatusNotFound)
		return
	}
	var msg db.OllamaMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if msg.Role == "" || msg.Content == "" {
		a.jsonError(w, "role and content are required", http.StatusBadRequest)
		return
	}
	msg.ConversationID = id
	if err := a.db.CreateOllamaMessage(&msg); err != nil {
		a.serverErr(w, "create message", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, msg)
}
