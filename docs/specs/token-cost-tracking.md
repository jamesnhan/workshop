# Token/Cost Tracking — Implementation Plan

**Card:** #677
**Status:** Planning — needs rework after #964
**Date:** 2026-04-12

> **Stale:** this plan hooks into `superviseDispatch()` and `completeDispatch()`,
> which were removed in #964 (agent-layer simplification). The `agent_usage`
> schema and Claude Code session-id mapping remain useful, but the
> collection trigger needs to move to a different site — likely the
> frontend telemetry already flowing through OTel, or a Claude Code
> SessionEnd hook that POSTs to `/debug/log`.

---

## 1. Database Schema

Add `agent_usage` table to `internal/db/db.go` in the `migrate()` function (~line 360).

```sql
CREATE TABLE IF NOT EXISTS agent_usage (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id    TEXT    NOT NULL,          -- Claude Code session UUID or agent session name
    pane_target   TEXT    NOT NULL DEFAULT '',-- tmux pane target (e.g. "agent-123:claude.0")
    provider      TEXT    NOT NULL,          -- claude, ollama, gemini, codex
    model         TEXT    NOT NULL,          -- claude-opus-4-6, gemma3:27b, etc.
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens    INTEGER NOT NULL DEFAULT 0,
    cache_create_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL    NOT NULL DEFAULT 0.0,
    project       TEXT    NOT NULL DEFAULT '',
    card_id       INTEGER DEFAULT 0,        -- kanban card if launched from one
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_usage_project   ON agent_usage(project);
CREATE INDEX IF NOT EXISTS idx_usage_provider  ON agent_usage(provider);
CREATE INDEX IF NOT EXISTS idx_usage_created   ON agent_usage(created_at);
CREATE INDEX IF NOT EXISTS idx_usage_session   ON agent_usage(session_id);
CREATE INDEX IF NOT EXISTS idx_usage_card      ON agent_usage(card_id);
```

**Go struct:**

```go
type AgentUsage struct {
    ID               int64   `json:"id"`
    SessionID        string  `json:"sessionId"`
    PaneTarget       string  `json:"paneTarget"`
    Provider         string  `json:"provider"`
    Model            string  `json:"model"`
    InputTokens      int64   `json:"inputTokens"`
    OutputTokens     int64   `json:"outputTokens"`
    CacheReadTokens  int64   `json:"cacheReadTokens"`
    CacheCreateTokens int64  `json:"cacheCreateTokens"`
    CostUSD          float64 `json:"costUsd"`
    Project          string  `json:"project"`
    CardID           int64   `json:"cardId"`
    CreatedAt        string  `json:"createdAt"`
}
```

**DB methods to add** (in `internal/db/db.go` or a new `internal/db/usage.go` for readability):

```go
func (d *DB) RecordUsage(u *AgentUsage) (int64, error)
func (d *DB) ListUsage(project, provider string, since, until string, limit int) ([]AgentUsage, error)
func (d *DB) AggregateUsage(project, provider, groupBy string, since, until string) ([]UsageAgg, error)
func (d *DB) DailySpend(project string, date string) (float64, error)
```

**`UsageAgg` struct** (for aggregation queries):

```go
type UsageAgg struct {
    GroupKey         string  `json:"groupKey"`     // date, provider, or project depending on groupBy
    TotalInput       int64   `json:"totalInput"`
    TotalOutput      int64   `json:"totalOutput"`
    TotalCacheRead   int64   `json:"totalCacheRead"`
    TotalCacheCreate int64   `json:"totalCacheCreate"`
    TotalCostUSD     float64 `json:"totalCostUsd"`
    EntryCount       int64   `json:"entryCount"`
}
```

---

## 2. Data Collection — Per Provider

### 2a. Claude Code (primary source)

**Data source:** `~/.claude/projects/{encoded-path}/{session-uuid}.jsonl`

Each assistant message contains a `usage` object:
```json
{
  "input_tokens": 1,
  "cache_creation_input_tokens": 514,
  "cache_read_input_tokens": 54071,
  "output_tokens": 250,
  "service_tier": "standard",
  "iterations": [{ "input_tokens": 1, "output_tokens": 250, ... }]
}
```

The `message.model` field gives the model name (e.g. `claude-opus-4-6`).

**Collection strategy — post-session scrape:**

When `superviseDispatch()` in `internal/api/v1/agents.go` detects an agent session has completed (line ~130), it currently calls `completeDispatch()`. Add a new step:

1. Resolve the session's Claude Code session UUID from the JSONL directory
2. Parse the JSONL file, sum all `usage` fields from `type: "assistant"` messages
3. Call `db.RecordUsage()` with the aggregated totals
4. The `project` comes from the JSONL `cwd` field (map to project name via directory basename)

**New function:** `internal/usage/claude.go`

```go
package usage

func ScrapeClaude(sessionDir, sessionID string) (*db.AgentUsage, error)
```

This scans `~/.claude/projects/` for matching JSONL files, parses assistant messages, and sums tokens. The session ID can be correlated via the `sessionId` field in JSONL entries.

**Challenge — mapping tmux session to Claude Code session UUID:**
- When we `launch_agent`, we know the tmux session name (e.g. `agent-1712345678`)
- The JSONL files are named by Claude Code's internal UUID, not the tmux session
- **Solution:** After agent launch, capture the pane and look for the session UUID in Claude Code's startup output, OR scan `~/.claude/sessions/*.json` which maps PIDs to sessionIDs — cross-reference with the PID running in the tmux pane (`tmux display-message -p '#{pane_pid}' -t <target>`)

**Simpler alternative — MCP self-report:**
Rather than scraping files, add an MCP tool `report_usage` that Claude Code agents call at session end (via a hook or prompt instruction). This is cleaner but requires cooperation from the agent. Could use both: MCP self-report as primary, file scrape as fallback/reconciliation.

**Recommendation:** Start with file scraping (more reliable, doesn't require agent cooperation), add self-report later.

### 2b. Ollama

**Data source:** Already in the response. `internal/mcp/mcp.go` lines 1668-1777.

Both `ollamaChatHandler` and `ollamaGenerateHandler` already parse `eval_count` and `eval_duration` from the response map. They format it as a markdown footer.

**Changes needed:**
1. In `ollamaChatHandler` and `ollamaGenerateHandler`, after extracting `eval_count`, also extract `prompt_eval_count` (input tokens)
2. Call `db.RecordUsage()` with provider=`ollama`, model from response
3. Cost for Ollama = $0 (local), but still track tokens for capacity planning

**Files to modify:**
- `internal/mcp/mcp.go:1668-1777` — add DB recording after response parsing
- Need to pass `*db.DB` into MCP handlers (currently they only get `tmux.Bridge`)

**Architecture note:** The MCP handlers currently receive only `bridge tmux.Bridge`. To record usage, they need DB access. Options:
- Pass `*db.DB` into the MCP `Serve()` function and thread it to handlers
- POST to the REST API `/api/v1/usage` from within MCP handlers (keeps MCP stateless, mirrors the ollama routing pattern)
- **Recommendation:** POST to REST API — consistent with how ollama handlers already proxy through the REST layer

### 2c. Gemini / Codex

**Data source:** Unknown — no local logs discovered. These providers are launched as subprocesses via tmux.

**Options:**
1. **Gemini CLI** may write logs — needs investigation (`~/.gemini/` or similar)
2. **Codex CLI** may write logs — needs investigation (`~/.codex/` or similar)
3. **Self-report MCP tool** — agents could call `report_usage` if instructed
4. **Capture-and-parse** — scrape the final terminal output for usage summaries (fragile)

**Recommendation:** Defer Gemini/Codex collection to Phase 2. Focus on Claude (highest spend) and Ollama (already available) first. Create placeholder support in the schema so the data model is ready.

**Decision needed:** James, should we investigate Gemini/Codex local log formats now, or defer?

---

## 3. Cost Calculation

**File:** `internal/usage/pricing.go`

Maintain a pricing table for known models:

```go
var Pricing = map[string]ModelPricing{
    "claude-opus-4-6":   {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheCreatePer1M: 18.75},
    "claude-sonnet-4-6": {InputPer1M: 3.00,  OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheCreatePer1M: 3.75},
    "claude-haiku-4-5":  {InputPer1M: 0.80,  OutputPer1M: 4.00,  CacheReadPer1M: 0.08, CacheCreatePer1M: 1.00},
    // Ollama models = $0
}

type ModelPricing struct {
    InputPer1M       float64
    OutputPer1M      float64
    CacheReadPer1M   float64
    CacheCreatePer1M float64
}

func CalcCost(model string, input, output, cacheRead, cacheCreate int64) float64
```

Ollama models default to $0. Unknown models default to $0 with a log warning.

---

## 4. Backend API

**File:** `internal/api/v1/usage.go` (new file)

### Endpoints

```
POST /api/v1/usage              — record a usage entry (for MCP self-report)
GET  /api/v1/usage              — list raw usage entries (filterable)
GET  /api/v1/usage/aggregate    — aggregated view (group by day/provider/project)
GET  /api/v1/usage/daily-spend  — single number: today's spend for budget checks
```

### Handler signatures

```go
func (a *API) handleRecordUsage(w http.ResponseWriter, r *http.Request)
func (a *API) handleListUsage(w http.ResponseWriter, r *http.Request)
func (a *API) handleAggregateUsage(w http.ResponseWriter, r *http.Request)
func (a *API) handleDailySpend(w http.ResponseWriter, r *http.Request)
```

### Query parameters

**`GET /usage`:**
- `project` — filter by project
- `provider` — filter by provider (claude, ollama, gemini, codex)
- `since` / `until` — ISO date range
- `limit` — max results (default 100)

**`GET /usage/aggregate`:**
- `group_by` — `day`, `provider`, `project`, `model` (required)
- `project`, `provider`, `since`, `until` — same filters

**`GET /usage/daily-spend`:**
- `project` — optional, filter by project
- `date` — optional, defaults to today

### Route registration

In `internal/api/v1/routes.go`:
```go
mux.HandleFunc("POST /usage", a.handleRecordUsage)
mux.HandleFunc("GET /usage", a.handleListUsage)
mux.HandleFunc("GET /usage/aggregate", a.handleAggregateUsage)
mux.HandleFunc("GET /usage/daily-spend", a.handleDailySpend)
```

---

## 5. Prometheus / OTel Metrics

**File:** `internal/telemetry/metrics.go`

Add two new metric instruments:

```go
AgentTokensTotal metric.Int64Counter    // labels: provider, model, project, direction (input/output/cache_read/cache_create)
AgentCostUSD     metric.Float64Counter  // labels: provider, model, project
```

**Recording points:**
- In `handleRecordUsage` (REST endpoint) — after DB insert
- In the Claude scraper — after computing totals
- In ollama handlers — after extracting eval_count

**Grafana dashboard** uses these via the existing OTLP → Mimir pipeline.

---

## 6. Frontend — Usage Dashboard

**File:** `frontend/src/components/UsageView.tsx` (new)

### Design

A new top-level view accessible via sidebar navigation (add "Usage" to the view list alongside Sessions, Kanban, Activity, etc.).

**Sections:**

1. **Summary cards** (top row)
   - Today's spend (USD)
   - This week's total tokens
   - Most active project
   - Most used model

2. **Cost by day** (bar chart, last 30 days)
   - Stacked by provider (Claude = blue, Ollama = gray, Gemini = green, Codex = orange)

3. **Tokens by project** (horizontal bar chart)
   - Input vs output breakdown

4. **Cost by provider** (donut/pie chart)

5. **Recent usage entries** (table, last 50)
   - Columns: time, session, provider, model, project, input tokens, output tokens, cost

### Charting library

**Decision needed:** The frontend currently has no charting library. Options:
- **Recharts** — React-native, declarative, well-maintained, ~180KB gzipped
- **Chart.js + react-chartjs-2** — canvas-based, good performance, ~60KB
- **Lightweight custom** — CSS bar charts for simple cases (no dependency)

**Recommendation:** Recharts — best DX for React, handles the chart types we need (bar, donut, line), and the bundle size is acceptable for a desktop-focused app.

### API integration

```typescript
// frontend/src/api/client.ts additions
export const getUsage = (params: UsageParams) => get<AgentUsage[]>('/usage', params)
export const getUsageAggregate = (params: AggParams) => get<UsageAgg[]>('/usage/aggregate', params)
export const getDailySpend = (project?: string) => get<{costUsd: number}>('/usage/daily-spend', { project })
```

### View switching

In `frontend/src/App.tsx`, add `"usage"` to the view enum and route it to `<UsageView />`.

In `internal/mcp/mcp.go`, add `"usage"` to the `switch_view` tool's allowed views.

---

## 7. Activity Integration

**File:** `internal/api/v1/agents.go` (modify `completeDispatch`)

After scraping usage on agent completion, include token count in the activity entry:

```go
// In completeDispatch, after usage scraping:
a.db.RecordActivity(&db.ActivityEntry{
    PaneTarget: disp.Target,
    ActionType: "agent_complete",
    Summary:    fmt.Sprintf("Agent completed — %dk tokens, $%.4f", totalTokens/1000, costUSD),
    Metadata:   marshalJSON(map[string]any{
        "inputTokens": usage.InputTokens,
        "outputTokens": usage.OutputTokens,
        "costUsd": usage.CostUSD,
        "model": usage.Model,
        "provider": usage.Provider,
    }),
    Project: project,
})
```

**Frontend:** In `ActivityView.tsx`, detect `agent_complete` action type and render token/cost badge alongside the entry. Parse `metadata` JSON to extract token counts.

---

## 8. Budget Alerts

**File:** `internal/usage/budget.go` (new)

### Configuration

Add budget config to Lua config (`internal/config/`):

```lua
usage = {
    daily_budget_usd = 50.00,        -- daily spend threshold
    alert_at_percent = 80,           -- warn at 80% of budget
    block_at_percent = 100,          -- require approval at 100%
    projects = {
        workshop = { daily_budget_usd = 20.00 },
    }
}
```

### Approval gate integration

Before launching an agent (in `handleLaunchAgent` or MCP `launch_agent`):

```go
func (b *BudgetChecker) CheckBudget(project string) (BudgetStatus, error)

type BudgetStatus struct {
    Allowed      bool
    DailySpend   float64
    DailyBudget  float64
    PercentUsed  float64
    NeedsApproval bool
}
```

If `NeedsApproval`, create an approval request via the existing `ApprovalHub`:
```go
a.approvals.Request("budget_exceeded", fmt.Sprintf(
    "Daily spend $%.2f exceeds budget $%.2f (%.0f%%). Allow agent launch?",
    status.DailySpend, status.DailyBudget, status.PercentUsed,
))
```

The approval UI already exists in `ActivityView.tsx` — it'll render the budget approval inline.

### Toast notification

When crossing the 80% threshold, fire `show_toast`:
```go
a.ui.Toast(fmt.Sprintf("Budget alert: $%.2f / $%.2f spent today (%.0f%%)", spend, budget, pct), "warning")
```

---

## 9. Implementation Order

### Phase 1: Foundation (do first)
| Step | What | Files | Depends on |
|------|------|-------|------------|
| 1.1 | `agent_usage` table + Go struct + CRUD methods | `internal/db/db.go` or `internal/db/usage.go` | nothing |
| 1.2 | Pricing table | `internal/usage/pricing.go` (new package) | nothing |
| 1.3 | REST endpoints (POST + GET + aggregate) | `internal/api/v1/usage.go`, `routes.go` | 1.1 |
| 1.4 | OTel metrics (counters) | `internal/telemetry/metrics.go` | nothing |

### Phase 2: Ollama Collection (quick win)
| Step | What | Files | Depends on |
|------|------|-------|------------|
| 2.1 | Record usage in ollama handlers | `internal/mcp/mcp.go:1668-1777` | 1.3 |

### Phase 3: Claude Scraping
| Step | What | Files | Depends on |
|------|------|-------|------------|
| 3.1 | JSONL parser + session ID resolution | `internal/usage/claude.go` (new) | 1.1, 1.2 |
| 3.2 | Hook into `completeDispatch` | `internal/api/v1/agents.go` | 3.1 |
| 3.3 | Activity entry enrichment | `internal/api/v1/agents.go` | 3.2 |

### Phase 4: Frontend
| Step | What | Files | Depends on |
|------|------|-------|------------|
| 4.1 | Install recharts (or chosen lib) | `frontend/package.json` | decision |
| 4.2 | API client functions | `frontend/src/api/client.ts` | 1.3 |
| 4.3 | UsageView component | `frontend/src/components/UsageView.tsx` | 4.1, 4.2 |
| 4.4 | Add "usage" to view routing | `frontend/src/App.tsx` | 4.3 |
| 4.5 | Add "usage" to switch_view MCP tool | `internal/mcp/mcp.go` | 4.4 |
| 4.6 | Activity token badges | `frontend/src/components/ActivityView.tsx` | 3.3 |

### Phase 5: Budget Alerts
| Step | What | Files | Depends on |
|------|------|-------|------------|
| 5.1 | Budget checker + Lua config | `internal/usage/budget.go`, `internal/config/` | 1.1 |
| 5.2 | Pre-launch budget gate | `internal/api/v1/agents.go` | 5.1 |
| 5.3 | Toast notifications | `internal/api/v1/agents.go` | 5.1 |

### Phase 6: Deferred
| Step | What | Notes |
|------|------|-------|
| 6.1 | Gemini/Codex log scraping | Investigate log formats when needed |
| 6.2 | MCP `report_usage` self-report tool | Supplement to file scraping |
| 6.3 | Grafana dashboard JSON | Once metrics are flowing |

---

## 10. Risks & Open Questions

### Risks

1. **Claude JSONL parsing is fragile** — file format is undocumented and could change between Claude Code versions. Mitigate: pin to known fields (`usage.input_tokens`, `usage.output_tokens`), fail gracefully on parse errors.

2. **Session ID mapping** — correlating tmux sessions to Claude Code session UUIDs requires cross-referencing `~/.claude/sessions/*.json` PID files. If Claude Code isn't running when we check, the PID file may be gone. Mitigate: capture PID at launch time and store in dispatch record.

3. **Pricing staleness** — hardcoded pricing will drift as Anthropic changes rates. Mitigate: make pricing configurable in Lua config, log warnings when unknown models appear.

4. **Cost for Max plan** — Claude Code under the Max plan has different (effectively $0 marginal) pricing. Need a config toggle for subscription vs API pricing. **Decision needed.**

### Open Questions

- [ ] **Charting library** — Recharts recommended, but open to alternatives. Approve?
- [ ] **Gemini/Codex** — investigate now or defer to Phase 6?
- [ ] **Max plan pricing** — should cost tracking show API list prices or $0 for subscription?
- [ ] **Budget defaults** — what daily budget makes sense as default? $50? $100?
- [ ] **Retention** — how long to keep raw usage entries? 90 days? Forever? Add a cleanup job?

---

## 11. Test Plan

### Backend tests (`internal/db/`)
- CRUD for `agent_usage` (create, list, aggregate)
- Aggregate grouping (by day, provider, project)
- Daily spend calculation
- Empty result sets

### Usage package tests (`internal/usage/`)
- Pricing calculation correctness (known models, unknown models, Ollama=$0)
- Claude JSONL parsing (valid, malformed, empty)
- Budget checker (under budget, at threshold, over budget)

### API tests (`internal/api/v1/`)
- POST /usage — valid, invalid, missing fields
- GET /usage — filters, pagination
- GET /usage/aggregate — each group_by mode
- GET /usage/daily-spend — with/without project filter

### Frontend tests (`frontend/src/`)
- UsageView renders with empty data
- UsageView renders with sample data
- Filter interactions
- API error handling

### Integration
- Ollama handler records usage after chat/generate
- Agent completion triggers Claude scraping + DB insert
- Budget gate blocks launch when over threshold
