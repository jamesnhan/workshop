package v1

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jamesnhan/workshop/internal/consensus"
	"github.com/jamesnhan/workshop/internal/tmux"
)

func (a *API) handleStartConsensus(w http.ResponseWriter, r *http.Request) {
	var req consensus.ConsensusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	result, err := a.consensus.StartRun(req)
	if err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, result)
}

func (a *API) handleGetConsensus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result := a.consensus.GetRun(id)
	if result == nil {
		a.jsonError(w, "not found", http.StatusNotFound)
		return
	}
	a.jsonOK(w, result)
}

func (a *API) handleCleanupConsensus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Kill all tmux sessions matching this consensus run
	sessions, _ := a.tmux.ListSessions()
	killed := 0

	// Also check unfiltered sessions (consensus- prefix is hidden from ListSessions)
	bridge, ok := a.tmux.(*tmux.ExecBridge)
	if ok {
		// List all sessions including hidden ones
		out, err := bridge.RunRaw("list-sessions", "-F", "#{session_name}")
		if err == nil {
			for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
				if strings.HasPrefix(name, id) {
					_ = a.tmux.KillSession(name)
					killed++
				}
			}
		}
	} else {
		// Fallback: try killing known patterns
		for _, s := range sessions {
			if strings.HasPrefix(s.Name, id) {
				_ = a.tmux.KillSession(s.Name)
				killed++
			}
		}
	}

	a.jsonOK(w, map[string]any{"killed": killed, "id": id})
}

func (a *API) handleListConsensus(w http.ResponseWriter, r *http.Request) {
	runs := a.consensus.ListRuns()
	if runs == nil {
		runs = []*consensus.ConsensusResult{}
	}
	a.jsonOK(w, runs)
}
