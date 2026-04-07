package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jamesnhan/workshop/internal/tmux"
)

type createSessionRequest struct {
	Name     string `json:"name"`
	StartDir string `json:"startDir,omitempty"`
}

type sendKeysRequest struct {
	Keys string `json:"keys"`
}

func (a *API) handleListSessions(w http.ResponseWriter, r *http.Request) {
	var sessions []tmux.Session
	var err error

	if r.URL.Query().Get("all") == "true" {
		if bridge, ok := a.tmux.(*tmux.ExecBridge); ok {
			sessions, err = bridge.ListAllSessions()
		} else {
			sessions, err = a.tmux.ListSessions()
		}
	} else {
		sessions, err = a.tmux.ListSessions()
	}
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, sessions)
}

func (a *API) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		a.jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := a.tmux.CreateSession(req.Name, req.StartDir); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, map[string]string{"name": req.Name})
}

func (a *API) handleKillSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.tmux.KillSession(name); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleSendKeys(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req sendKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := a.tmux.SendKeys(name, req.Keys); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"status": "sent"})
}

func (a *API) handleCapturePane(w http.ResponseWriter, r *http.Request) {
	// Use ?target= query param if provided, otherwise fall back to session name
	target := r.URL.Query().Get("target")
	if target == "" {
		target = r.PathValue("name")
	}
	lines := 50
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			lines = n
		}
	}
	output, err := a.tmux.CapturePane(target, lines)
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"output": output})
}

func (a *API) handleListPanes(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	panes, err := a.tmux.ListPanes(name)
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, panes)
}

func (a *API) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewName == "" {
		a.jsonError(w, "newName is required", http.StatusBadRequest)
		return
	}
	if err := a.tmux.RenameSession(name, req.NewName); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"name": req.NewName})
}

func (a *API) handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("name")
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if err := a.tmux.CreateWindow(session, req.Name); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, map[string]string{"status": "created"})
}

func (a *API) handleRenameWindow(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("target")
	var req struct {
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewName == "" {
		a.jsonError(w, "newName is required", http.StatusBadRequest)
		return
	}
	if err := a.tmux.RenameWindow(target, req.NewName); err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	a.jsonOK(w, map[string]string{"name": req.NewName})
}

func (a *API) handleListAllPanes(w http.ResponseWriter, r *http.Request) {
	// Include hidden sessions (consensus-*, workshop-ctrl-*) so the agent
	// dashboard can detect consensus agents.
	var sessions []tmux.Session
	var err error
	if eb, ok := a.tmux.(*tmux.ExecBridge); ok {
		sessions, err = eb.ListAllSessions()
	} else {
		sessions, err = a.tmux.ListSessions()
	}
	if err != nil {
		a.serverErr(w, "operation failed", err)
		return
	}
	var allPanes []any
	for _, s := range sessions {
		panes, err := a.tmux.ListPanes(s.Name)
		if err != nil {
			continue
		}
		for _, p := range panes {
			allPanes = append(allPanes, p)
		}
	}
	if allPanes == nil {
		allPanes = []any{}
	}
	a.jsonOK(w, allPanes)
}

func (a *API) jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (a *API) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// serverErr logs the real error and returns a safe generic message to the client.
func (a *API) serverErr(w http.ResponseWriter, context string, err error) {
	a.logger.Error(context, "err", err)
	a.jsonError(w, "internal server error", http.StatusInternalServerError)
}
