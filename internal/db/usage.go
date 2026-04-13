package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jamesnhan/workshop/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// AgentUsage records token consumption for a single agent interaction.
type AgentUsage struct {
	ID                int64   `json:"id"`
	SessionID         string  `json:"sessionId"`
	PaneTarget        string  `json:"paneTarget"`
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	InputTokens       int64   `json:"inputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	CacheReadTokens   int64   `json:"cacheReadTokens"`
	CacheCreateTokens int64   `json:"cacheCreateTokens"`
	CostUSD           float64 `json:"costUsd"`
	Project           string  `json:"project"`
	CardID            int64   `json:"cardId"`
	CreatedAt         string  `json:"createdAt"`
}

// UsageAgg holds aggregated usage data grouped by a key.
type UsageAgg struct {
	GroupKey         string  `json:"groupKey"`
	TotalInput       int64   `json:"totalInput"`
	TotalOutput      int64   `json:"totalOutput"`
	TotalCacheRead   int64   `json:"totalCacheRead"`
	TotalCacheCreate int64   `json:"totalCacheCreate"`
	TotalCostUSD     float64 `json:"totalCostUsd"`
	EntryCount       int64   `json:"entryCount"`
}

// RecordUsage inserts a usage entry and returns its ID.
func (d *DB) RecordUsage(u *AgentUsage) (int64, error) {
	_, span := telemetry.Tracer("db").Start(context.Background(), "db.RecordUsage",
		telemetry.Attrs(
			attribute.String("workshop.usage.provider", u.Provider),
			attribute.String("workshop.usage.model", u.Model),
		),
	)
	defer span.End()

	result, err := d.db.Exec(
		`INSERT INTO agent_usage (session_id, pane_target, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, project, card_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.SessionID, u.PaneTarget, u.Provider, u.Model,
		u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheCreateTokens,
		u.CostUSD, u.Project, u.CardID,
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	u.ID = id
	return id, nil
}

// ListUsage returns raw usage entries with optional filters.
func (d *DB) ListUsage(project, provider, since, until string, limit int) ([]AgentUsage, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT id, session_id, pane_target, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, project, card_id, created_at FROM agent_usage`
	var where []string
	var args []any

	if project != "" {
		where = append(where, `project = ?`)
		args = append(args, project)
	}
	if provider != "" {
		where = append(where, `provider = ?`)
		args = append(args, provider)
	}
	if since != "" {
		where = append(where, `created_at >= ?`)
		args = append(args, since)
	}
	if until != "" {
		where = append(where, `created_at <= ?`)
		args = append(args, until)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AgentUsage
	for rows.Next() {
		var u AgentUsage
		if err := rows.Scan(&u.ID, &u.SessionID, &u.PaneTarget, &u.Provider, &u.Model,
			&u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheCreateTokens,
			&u.CostUSD, &u.Project, &u.CardID, &u.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, u)
	}
	return entries, rows.Err()
}

// AggregateUsage returns grouped usage summaries. Valid groupBy values: day, provider, project, model.
func (d *DB) AggregateUsage(project, provider, groupBy, since, until string) ([]UsageAgg, error) {
	var groupExpr string
	switch groupBy {
	case "day":
		groupExpr = `date(created_at)`
	case "provider":
		groupExpr = `provider`
	case "project":
		groupExpr = `project`
	case "model":
		groupExpr = `model`
	default:
		return nil, fmt.Errorf("invalid group_by: %s (must be day, provider, project, or model)", groupBy)
	}

	query := fmt.Sprintf(`SELECT %s AS group_key,
		SUM(input_tokens), SUM(output_tokens),
		SUM(cache_read_tokens), SUM(cache_create_tokens),
		SUM(cost_usd), COUNT(*)
		FROM agent_usage`, groupExpr)

	var where []string
	var args []any
	if project != "" {
		where = append(where, `project = ?`)
		args = append(args, project)
	}
	if provider != "" {
		where = append(where, `provider = ?`)
		args = append(args, provider)
	}
	if since != "" {
		where = append(where, `created_at >= ?`)
		args = append(args, since)
	}
	if until != "" {
		where = append(where, `created_at <= ?`)
		args = append(args, until)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += fmt.Sprintf(` GROUP BY %s ORDER BY group_key`, groupExpr)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aggs []UsageAgg
	for rows.Next() {
		var a UsageAgg
		if err := rows.Scan(&a.GroupKey, &a.TotalInput, &a.TotalOutput,
			&a.TotalCacheRead, &a.TotalCacheCreate, &a.TotalCostUSD, &a.EntryCount); err != nil {
			return nil, err
		}
		aggs = append(aggs, a)
	}
	return aggs, rows.Err()
}

// DailySpend returns the total USD spend for a given date (YYYY-MM-DD), optionally filtered by project.
func (d *DB) DailySpend(project, date string) (float64, error) {
	query := `SELECT COALESCE(SUM(cost_usd), 0) FROM agent_usage WHERE date(created_at) = ?`
	args := []any{date}
	if project != "" {
		query += ` AND project = ?`
		args = append(args, project)
	}
	var total float64
	err := d.db.QueryRow(query, args...).Scan(&total)
	return total, err
}
