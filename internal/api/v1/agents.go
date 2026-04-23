package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/tmux"
)

func (a *API) handleLaunchAgent(w http.ResponseWriter, r *http.Request) {
	if !a.requireTmux(w) {
		return
	}
	var cfg tmux.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate model name before launching to prevent command injection.
	if err := tmux.ValidateModelName(cfg.Model); err != nil {
		a.jsonError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	result, err := a.tmux.LaunchAgent(cfg)
	if err != nil {
		a.serverErr(w, "agent launch failed", err)
		return
	}

	// Pre-register so the monitor doesn't double-broadcast.
	a.status.MarkSeen(result.Target)
	a.status.Broadcast("session_created", map[string]any{
		"target":     result.Target,
		"session":    result.SessionName,
		"background": cfg.Background,
	})

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, result)
}
