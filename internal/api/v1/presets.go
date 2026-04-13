package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleListPresets(w http.ResponseWriter, r *http.Request) {
	presets, err := a.db.ListPresets()
	if err != nil {
		a.serverErr(w, "list presets", err)
		return
	}
	if presets == nil {
		presets = []db.AgentPreset{}
	}
	a.jsonOK(w, presets)
}

func (a *API) handleUpsertPreset(w http.ResponseWriter, r *http.Request) {
	var p db.AgentPreset
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if p.Name == "" {
		a.jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := a.db.UpsertPreset(&p); err != nil {
		a.serverErr(w, "upsert preset", err)
		return
	}
	a.jsonOK(w, p)
}

func (a *API) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.db.DeletePreset(name); err != nil {
		a.serverErr(w, "delete preset", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
