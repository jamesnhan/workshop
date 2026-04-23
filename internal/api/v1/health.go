package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/tmux"
)

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	_, isNoBridge := a.tmux.(*tmux.NoBridge)
	headless := isNoBridge
	hasProxy := a.tmuxProxy != nil

	resp := map[string]any{
		"status":   "ok",
		"headless": headless,
		"features": map[string]bool{
			"tmux":     !headless || hasProxy,
			"kanban":   true,
			"ollama":   a.ollama != nil,
			"channels": true,
		},
	}
	if hasProxy {
		resp["tmuxProxy"] = a.tmuxProxy.target.String()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
