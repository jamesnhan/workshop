package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/telemetry"
	"github.com/jamesnhan/workshop/internal/usage"
	"go.opentelemetry.io/otel/attribute"
)

func (a *API) handleRecordUsage(w http.ResponseWriter, r *http.Request) {
	var u db.AgentUsage
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if u.Provider == "" || u.Model == "" {
		a.jsonError(w, "provider and model are required", http.StatusBadRequest)
		return
	}
	if u.SessionID == "" {
		u.SessionID = "unknown"
	}

	// Calculate cost if not provided
	if u.CostUSD == 0 {
		u.CostUSD = usage.CalcCost(u.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheCreateTokens)
	}

	if _, err := a.db.RecordUsage(&u); err != nil {
		a.serverErr(w, "record usage", err)
		return
	}

	// Emit OTel metrics
	ctx := context.Background()
	attrs := telemetry.MetricAttrs(
		attribute.String("provider", u.Provider),
		attribute.String("model", u.Model),
		attribute.String("project", u.Project),
	)
	inputAttrs := telemetry.MetricAttrs(
		attribute.String("provider", u.Provider),
		attribute.String("model", u.Model),
		attribute.String("project", u.Project),
		attribute.String("direction", "input"),
	)
	outputAttrs := telemetry.MetricAttrs(
		attribute.String("provider", u.Provider),
		attribute.String("model", u.Model),
		attribute.String("project", u.Project),
		attribute.String("direction", "output"),
	)
	cacheReadAttrs := telemetry.MetricAttrs(
		attribute.String("provider", u.Provider),
		attribute.String("model", u.Model),
		attribute.String("project", u.Project),
		attribute.String("direction", "cache_read"),
	)
	cacheCreateAttrs := telemetry.MetricAttrs(
		attribute.String("provider", u.Provider),
		attribute.String("model", u.Model),
		attribute.String("project", u.Project),
		attribute.String("direction", "cache_create"),
	)

	telemetry.AgentTokensTotal.Add(ctx, u.InputTokens, inputAttrs)
	telemetry.AgentTokensTotal.Add(ctx, u.OutputTokens, outputAttrs)
	telemetry.AgentTokensTotal.Add(ctx, u.CacheReadTokens, cacheReadAttrs)
	telemetry.AgentTokensTotal.Add(ctx, u.CacheCreateTokens, cacheCreateAttrs)
	telemetry.AgentCostUSD.Add(ctx, u.CostUSD, attrs)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(u)
}

func (a *API) handleListUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project := q.Get("project")
	provider := q.Get("provider")
	since := q.Get("since")
	until := q.Get("until")
	limit := 100
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := a.db.ListUsage(project, provider, since, until, limit)
	if err != nil {
		a.serverErr(w, "list usage", err)
		return
	}
	if entries == nil {
		entries = []db.AgentUsage{}
	}
	a.jsonOK(w, entries)
}

func (a *API) handleAggregateUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupBy := q.Get("group_by")
	if groupBy == "" {
		a.jsonError(w, "group_by is required (day, provider, project, model)", http.StatusBadRequest)
		return
	}
	project := q.Get("project")
	provider := q.Get("provider")
	since := q.Get("since")
	until := q.Get("until")

	aggs, err := a.db.AggregateUsage(project, provider, groupBy, since, until)
	if err != nil {
		a.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if aggs == nil {
		aggs = []db.UsageAgg{}
	}
	a.jsonOK(w, aggs)
}

func (a *API) handleDailySpend(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project := q.Get("project")
	date := q.Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	total, err := a.db.DailySpend(project, date)
	if err != nil {
		a.serverErr(w, "daily spend", err)
		return
	}
	a.jsonOK(w, map[string]any{"costUsd": total, "date": date, "project": project})
}
