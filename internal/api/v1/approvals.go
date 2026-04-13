package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// handleRequestApproval creates a pending approval, broadcasts it to the
// frontend, and blocks until the user approves or denies (up to 10 minutes).
func (a *API) handleRequestApproval(w http.ResponseWriter, r *http.Request) {
	var req db.ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Action == "" {
		a.jsonError(w, "action is required", http.StatusBadRequest)
		return
	}

	// Persist to DB
	id, err := a.db.CreateApproval(&req)
	if err != nil {
		a.serverErr(w, "create approval", err)
		return
	}
	req.ID = id
	req.Status = "pending"

	telemetry.ApprovalRequestsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(
			attribute.String("action", req.Action),
			attribute.String("decision", "pending"),
		),
	)

	if a.approvals == nil {
		a.jsonError(w, "approval hub not available", http.StatusServiceUnavailable)
		return
	}

	// Block until the user responds in the UI or timeout
	decision := a.approvals.WaitForDecision(id, map[string]any{
		"approvalId": id,
		"paneTarget": req.PaneTarget,
		"agentName":  req.AgentName,
		"action":     req.Action,
		"details":    req.Details,
		"diff":       req.Diff,
		"project":    req.Project,
	}, 10*time.Minute)

	if decision == "timeout" {
		a.db.ResolveApproval(id, "denied")
		telemetry.ApprovalRequestsTotal.Add(context.Background(), 1,
			telemetry.MetricAttrs(
				attribute.String("action", req.Action),
				attribute.String("decision", "timeout"),
			),
		)
		a.jsonOK(w, map[string]any{"id": id, "status": "denied", "reason": "timeout"})
		return
	}

	a.db.ResolveApproval(id, decision)
	telemetry.ApprovalRequestsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(
			attribute.String("action", req.Action),
			attribute.String("decision", decision),
		),
	)

	a.jsonOK(w, map[string]any{"id": id, "status": decision})
}

// handleResolveApproval lets the frontend approve or deny by approval DB ID.
func (a *API) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Decision string `json:"decision"` // "approved" or "denied"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Decision != "approved" && req.Decision != "denied" {
		a.jsonError(w, "decision must be 'approved' or 'denied'", http.StatusBadRequest)
		return
	}

	if a.approvals != nil {
		if !a.approvals.Resolve(id, req.Decision) {
			// Not in the live pending map — try DB-only resolve (stale request)
			if err := a.db.ResolveApproval(id, req.Decision); err != nil {
				a.jsonError(w, err.Error(), http.StatusNotFound)
				return
			}
		}
	} else {
		if err := a.db.ResolveApproval(id, req.Decision); err != nil {
			a.jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	a.jsonOK(w, map[string]any{"id": id, "status": req.Decision})
}

func (a *API) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	reqs, err := a.db.ListApprovals(status, limit)
	if err != nil {
		a.serverErr(w, "list approvals", err)
		return
	}
	if reqs == nil {
		reqs = []db.ApprovalRequest{}
	}
	a.jsonOK(w, reqs)
}
