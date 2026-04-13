package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	wf, err := a.db.GetOrDefaultWorkflow(project)
	if err != nil {
		a.serverErr(w, "get workflow", err)
		return
	}
	a.jsonOK(w, wf)
}

func (a *API) handleSetWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string            `json:"project"`
		Config  db.WorkflowConfig `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Project == "" {
		a.jsonError(w, "project is required", http.StatusBadRequest)
		return
	}
	if len(req.Config.Columns) == 0 {
		a.jsonError(w, "at least one column is required", http.StatusBadRequest)
		return
	}
	if req.Config.Transitions == nil {
		req.Config.Transitions = map[string][]string{}
	}
	if err := a.db.SetWorkflow(req.Project, &req.Config); err != nil {
		a.serverErr(w, "set workflow", err)
		return
	}
	a.jsonOK(w, req.Config)
}
