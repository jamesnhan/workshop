package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

func (a *API) handleRecordActivity(w http.ResponseWriter, r *http.Request) {
	var entry db.ActivityEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if entry.ActionType == "" || entry.Summary == "" {
		a.jsonError(w, "actionType and summary are required", http.StatusBadRequest)
		return
	}

	id, err := a.db.RecordActivity(&entry)
	if err != nil {
		a.serverErr(w, "record activity", err)
		return
	}
	entry.ID = id

	// Emit telemetry
	telemetry.ActivityEventsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(
			attribute.String("action_type", entry.ActionType),
			attribute.String("project", entry.Project),
		),
	)

	// Broadcast to all connected WebSocket clients for live streaming
	if a.status != nil {
		a.status.Broadcast("activity", entry)
	}

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, entry)
}

func (a *API) handleListActivity(w http.ResponseWriter, r *http.Request) {
	pane := r.URL.Query().Get("pane")
	project := r.URL.Query().Get("project")
	actionType := r.URL.Query().Get("action_type")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	tree := r.URL.Query().Get("tree") == "true"
	entries, err := a.db.ListActivity(pane, project, actionType, limit, tree)
	if err != nil {
		a.serverErr(w, "list activity", err)
		return
	}
	if entries == nil {
		entries = []db.ActivityEntry{}
	}
	a.jsonOK(w, entries)
}
