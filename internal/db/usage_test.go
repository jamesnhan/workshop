package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedUsage(t *testing.T, d *db.DB, provider, model, project string, input, output int64, cost float64) int64 {
	t.Helper()
	u := &db.AgentUsage{
		SessionID:   "test-session",
		PaneTarget:  "test:0.0",
		Provider:    provider,
		Model:       model,
		InputTokens: input,
		OutputTokens: output,
		CostUSD:     cost,
		Project:     project,
	}
	id, err := d.RecordUsage(u)
	require.NoError(t, err)
	return id
}

func TestRecordUsage(t *testing.T) {
	d := testhelpers.TempDB(t)
	u := &db.AgentUsage{
		SessionID:         "sess-1",
		PaneTarget:        "agent-1:claude.0",
		Provider:          "claude",
		Model:             "claude-opus-4-6",
		InputTokens:       1000,
		OutputTokens:      500,
		CacheReadTokens:   5000,
		CacheCreateTokens: 200,
		CostUSD:           0.075,
		Project:           "workshop",
		CardID:            42,
	}
	id, err := d.RecordUsage(u)
	require.NoError(t, err)
	assert.NotZero(t, id)
	assert.Equal(t, id, u.ID)
}

func TestListUsage_noFilters(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.01)
	seedUsage(t, d, "ollama", "gemma3:27b", "workshop", 200, 100, 0)

	entries, err := d.ListUsage("", "", "", "", 0)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestListUsage_projectFilter(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.01)
	seedUsage(t, d, "claude", "opus", "roblox", 200, 100, 0.02)

	entries, err := d.ListUsage("workshop", "", "", "", 0)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "workshop", entries[0].Project)
}

func TestListUsage_providerFilter(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.01)
	seedUsage(t, d, "ollama", "gemma3:27b", "workshop", 200, 100, 0)

	entries, err := d.ListUsage("", "ollama", "", "", 0)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "ollama", entries[0].Provider)
}

func TestListUsage_limit(t *testing.T) {
	d := testhelpers.TempDB(t)
	for i := 0; i < 10; i++ {
		seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.01)
	}

	entries, err := d.ListUsage("", "", "", "", 3)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

func TestListUsage_empty(t *testing.T) {
	d := testhelpers.TempDB(t)
	entries, err := d.ListUsage("", "", "", "", 0)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestListUsage_sinceUntilFilter(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.01)
	seedUsage(t, d, "claude", "opus", "workshop", 200, 100, 0.02)

	// Grab actual created_at values so the test isn't coupled to wall clock
	all, err := d.ListUsage("", "", "", "", 0)
	require.NoError(t, err)
	require.Len(t, all, 2)
	date := all[0].CreatedAt[:10]

	// since=today should return both entries
	entries, err := d.ListUsage("", "", date, "", 0)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// until=yesterday should return nothing
	entries, err = d.ListUsage("", "", "", "1970-01-01", 0)
	require.NoError(t, err)
	assert.Nil(t, entries)

	// since=tomorrow should return nothing
	entries, err = d.ListUsage("", "", "2099-01-01", "", 0)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestAggregateUsage_byProvider(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.10)
	seedUsage(t, d, "claude", "opus", "workshop", 200, 100, 0.20)
	seedUsage(t, d, "ollama", "gemma3:27b", "workshop", 300, 150, 0)

	aggs, err := d.AggregateUsage("", "", "provider", "", "")
	require.NoError(t, err)
	assert.Len(t, aggs, 2)

	// Find the claude entry
	var claudeAgg db.UsageAgg
	for _, a := range aggs {
		if a.GroupKey == "claude" {
			claudeAgg = a
		}
	}
	assert.Equal(t, int64(300), claudeAgg.TotalInput)
	assert.Equal(t, int64(150), claudeAgg.TotalOutput)
	assert.Equal(t, int64(2), claudeAgg.EntryCount)
	assert.InDelta(t, 0.30, claudeAgg.TotalCostUSD, 0.001)
}

func TestAggregateUsage_byProject(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.10)
	seedUsage(t, d, "claude", "opus", "roblox", 200, 100, 0.20)

	aggs, err := d.AggregateUsage("", "", "project", "", "")
	require.NoError(t, err)
	assert.Len(t, aggs, 2)
}

func TestAggregateUsage_byModel(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.10)
	seedUsage(t, d, "claude", "sonnet", "workshop", 200, 100, 0.05)

	aggs, err := d.AggregateUsage("", "", "model", "", "")
	require.NoError(t, err)
	assert.Len(t, aggs, 2)
}

func TestAggregateUsage_invalidGroupBy(t *testing.T) {
	d := testhelpers.TempDB(t)
	_, err := d.AggregateUsage("", "", "invalid", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid group_by")
}

func TestAggregateUsage_empty(t *testing.T) {
	d := testhelpers.TempDB(t)
	aggs, err := d.AggregateUsage("", "", "provider", "", "")
	require.NoError(t, err)
	assert.Nil(t, aggs)
}

func TestDailySpend(t *testing.T) {
	d := testhelpers.TempDB(t)
	seedUsage(t, d, "claude", "opus", "workshop", 100, 50, 0.10)
	seedUsage(t, d, "claude", "opus", "workshop", 200, 100, 0.20)
	seedUsage(t, d, "claude", "opus", "roblox", 300, 150, 0.30)

	// Get today's date in the same format SQLite uses
	entries, err := d.ListUsage("", "", "", "", 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	// Extract just the date part
	date := entries[0].CreatedAt[:10]

	total, err := d.DailySpend("", date)
	require.NoError(t, err)
	assert.InDelta(t, 0.60, total, 0.001)

	// With project filter
	total, err = d.DailySpend("workshop", date)
	require.NoError(t, err)
	assert.InDelta(t, 0.30, total, 0.001)
}

func TestDailySpend_noData(t *testing.T) {
	d := testhelpers.TempDB(t)
	total, err := d.DailySpend("", "2099-01-01")
	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
}
