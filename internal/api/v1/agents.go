package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/tmux"
)

func (a *API) handleListProviders(w http.ResponseWriter, r *http.Request) {
	a.jsonOK(w, tmux.AvailableProviders())
}

func (a *API) handleLaunchAgent(w http.ResponseWriter, r *http.Request) {
	var cfg tmux.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := a.tmux.LaunchAgent(cfg)
	if err != nil {
		a.serverErr(w, "agent launch failed", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, result)
}
