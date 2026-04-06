package v1

import (
	"encoding/json"
	"net/http"
)

func (a *API) handleSetPaneStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target  string `json:"target"`
		Status  string `json:"status"`  // green, yellow, red
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Target == "" || req.Status == "" {
		a.jsonError(w, "target and status required", http.StatusBadRequest)
		return
	}
	switch req.Status {
	case "green", "yellow", "red":
	default:
		a.jsonError(w, "status must be green, yellow, or red", http.StatusBadRequest)
		return
	}
	a.status.Set(req.Target, req.Status, req.Message)
	a.jsonOK(w, map[string]string{"status": "ok"})
}

func (a *API) handleClearPaneStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Target == "" {
		a.jsonError(w, "target required", http.StatusBadRequest)
		return
	}
	a.status.Clear(req.Target)
	a.jsonOK(w, map[string]string{"status": "ok"})
}
