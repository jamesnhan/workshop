package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"path/filepath"
	"time"

	"github.com/jamesnhan/workshop/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Card struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Column      string    `json:"column"`      // backlog, in_progress, review, done
	Project     string    `json:"project"`      // project/workspace name for filtering
	Position    int       `json:"position"`     // order within column
	PaneTarget  string    `json:"paneTarget"`   // linked tmux pane target (optional)
	Labels      string    `json:"labels"`       // comma-separated labels
	CardType    string    `json:"cardType"`     // bug, feature, task, chore
	Priority    string    `json:"priority"`     // P0, P1, P2, P3
	ParentID    int64     `json:"parentId"`     // 0 if root; otherwise the parent card's ID
	Archived    bool      `json:"archived"`     // hidden from default list views
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// --- Workflow types ---

type WorkflowColumn struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// TransitionGate defines validation rules that a card must satisfy before
// a specific column transition is allowed.
type TransitionGate struct {
	RequireDescription bool `json:"require_description,omitempty"` // card must have non-empty description
	RequireChecklist   bool `json:"require_checklist,omitempty"`   // description must contain at least one "- [ ]" checkbox
}

type WorkflowConfig struct {
	Columns     []WorkflowColumn        `json:"columns"`
	Transitions map[string][]string     `json:"transitions"`
	Gates       map[string]TransitionGate `json:"gates,omitempty"` // key: "from→to" e.g. "backlog→in_progress"
}

// DefaultWorkflow is the standard 4-column kanban used when a project has no custom workflow.
var DefaultWorkflow = WorkflowConfig{
	Columns: []WorkflowColumn{
		{ID: "backlog", Label: "Backlog"},
		{ID: "in_progress", Label: "In Progress"},
		{ID: "review", Label: "Review"},
		{ID: "done", Label: "Done"},
	},
	Transitions: map[string][]string{
		"backlog":     {"in_progress"},
		"in_progress": {"review", "backlog"},
		"review":      {"done", "in_progress"},
		"done":        {"backlog"},
	},
	Gates: map[string]TransitionGate{
		"backlog→in_progress": {RequireDescription: true},
	},
}

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "workshop.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode for better concurrent reads
	sqlDB.Exec("PRAGMA journal_mode=WAL")
	// Enforce ON DELETE CASCADE foreign keys — SQLite is off by default.
	sqlDB.Exec("PRAGMA foreign_keys=ON")

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS cards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			"column" TEXT NOT NULL DEFAULT 'backlog',
			project TEXT DEFAULT '',
			position INTEGER DEFAULT 0,
			pane_target TEXT DEFAULT '',
			labels TEXT DEFAULT '',
			card_type TEXT DEFAULT '',
			priority TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}
	// Add columns if they don't exist (for existing databases)
	d.db.Exec(`ALTER TABLE cards ADD COLUMN card_type TEXT DEFAULT ''`)
	d.db.Exec(`ALTER TABLE cards ADD COLUMN priority TEXT DEFAULT ''`)
	d.db.Exec(`ALTER TABLE cards ADD COLUMN parent_id INTEGER DEFAULT 0`)
	d.db.Exec(`ALTER TABLE cards ADD COLUMN archived INTEGER DEFAULT 0`)
	// Auto-archive existing done cards (one-time migration, idempotent)
	d.db.Exec(`UPDATE cards SET archived = 1 WHERE "column" = 'done' AND archived = 0`)

	// Recordings table
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS recordings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			target TEXT DEFAULT '',
			cols INTEGER DEFAULT 80,
			rows INTEGER DEFAULT 24,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			duration_ms INTEGER DEFAULT 0,
			status TEXT DEFAULT 'recording'
		)
	`)
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS card_notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			card_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE
		)
	`)

	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS recording_frames (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recording_id INTEGER NOT NULL,
			offset_ms INTEGER NOT NULL,
			data BLOB NOT NULL,
			FOREIGN KEY (recording_id) REFERENCES recordings(id) ON DELETE CASCADE
		)
	`)

	// Card activity log — tracks all changes to cards over time
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS card_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			card_id INTEGER NOT NULL,
			action TEXT NOT NULL,
			before_value TEXT DEFAULT '',
			after_value TEXT DEFAULT '',
			source TEXT DEFAULT 'user',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE
		)
	`)

	// Consensus runs
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS consensus_runs (
			id TEXT PRIMARY KEY,
			prompt TEXT NOT NULL,
			directory TEXT DEFAULT '',
			status TEXT DEFAULT 'running',
			coordinator_pane TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS consensus_agents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			name TEXT NOT NULL,
			model TEXT DEFAULT '',
			provider TEXT DEFAULT '',
			target TEXT DEFAULT '',
			status TEXT DEFAULT 'running',
			output TEXT DEFAULT '',
			needs_input INTEGER DEFAULT 0,
			input_prompt TEXT DEFAULT '',
			FOREIGN KEY (run_id) REFERENCES consensus_runs(id) ON DELETE CASCADE
		)
	`)

	// Card dispatches — tracks agents launched from cards
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS card_dispatches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			card_id INTEGER NOT NULL,
			session_name TEXT NOT NULL,
			target TEXT NOT NULL,
			provider TEXT DEFAULT 'claude',
			status TEXT DEFAULT 'running',
			auto_cleanup INTEGER DEFAULT 1,
			dispatched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_dispatches_card ON card_dispatches(card_id, dispatched_at DESC)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_dispatches_status ON card_dispatches(status)`)
	d.db.Exec(`ALTER TABLE card_dispatches ADD COLUMN worktree_dir TEXT DEFAULT ''`)
	d.db.Exec(`ALTER TABLE card_dispatches ADD COLUMN branch TEXT DEFAULT ''`)

	// Channel subscriptions: pane X wants messages on channel Y
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS channel_subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			target TEXT NOT NULL,
			project TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(channel, target)
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_chan_subs_channel ON channel_subscriptions(channel)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_chan_subs_target ON channel_subscriptions(target)`)

	// Channel messages: persisted history
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS channel_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			sender TEXT DEFAULT '',
			body TEXT NOT NULL,
			project TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_chan_msgs_channel ON channel_messages(channel, created_at DESC)`)

	// Per-project workflow definitions — column order + allowed transitions.
	// Config is a JSON blob:
	// {
	//   "columns": [{"id": "backlog", "label": "Backlog"}, ...],
	//   "transitions": {"backlog": ["in_progress"], "in_progress": ["review", "backlog"], ...}
	// }
	// If no workflow exists for a project, the default 4-column workflow is used.
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project TEXT NOT NULL UNIQUE,
			config TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)

	// Seed default workflow for any existing projects that don't have one yet.
	d.seedDefaultWorkflows()

	// Indexes for hot query paths
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cards_proj_col ON cards(project, "column", position)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notes_card ON card_notes(card_id, created_at)`)

	// Card messages — threaded chat-style comments (separate from append-only notes)
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS card_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			card_id INTEGER NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_card ON card_messages(card_id, created_at)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rec_frames ON recording_frames(recording_id, offset_ms)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_consensus_agents_run ON consensus_agents(run_id)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_card_log_card ON card_log(card_id, created_at DESC)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cards_parent ON cards(parent_id)`)

	// Card dependencies — directed edges: blocker_id blocks blocked_id
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS card_dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			blocker_id INTEGER NOT NULL,
			blocked_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(blocker_id, blocked_id),
			FOREIGN KEY (blocker_id) REFERENCES cards(id) ON DELETE CASCADE,
			FOREIGN KEY (blocked_id) REFERENCES cards(id) ON DELETE CASCADE
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_dep_blocker ON card_dependencies(blocker_id)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_dep_blocked ON card_dependencies(blocked_id)`)

	// Activity log — tracks agent actions for visibility / audit
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS activity_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pane_target TEXT DEFAULT '',
			agent_name TEXT DEFAULT '',
			action_type TEXT NOT NULL,
			summary TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			project TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_created ON activity_log(created_at DESC)`)
	d.db.Exec(`ALTER TABLE activity_log ADD COLUMN parent_id INTEGER DEFAULT 0`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_parent ON activity_log(parent_id)`)

	// Approval requests — blocking agent requests that need user approve/deny
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS approval_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pane_target TEXT DEFAULT '',
			agent_name TEXT DEFAULT '',
			action TEXT NOT NULL,
			details TEXT DEFAULT '',
			diff TEXT DEFAULT '',
			project TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			decided_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_approval_status ON approval_requests(status, created_at DESC)`)

	// Agent presets — reusable named configurations for launching specialist agents
	// Agent presets — reusable named configurations for launching specialist agents
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_presets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			provider TEXT DEFAULT 'claude',
			model TEXT DEFAULT '',
			prompt TEXT DEFAULT '',
			system_prompt TEXT DEFAULT '',
			directory TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.seedDefaultPresets()
	d.seedOrchestrator()
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_pane ON activity_log(pane_target, created_at DESC)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_project ON activity_log(project, created_at DESC)`)

	// Agent usage tracking — token/cost data per agent interaction
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_usage (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id        TEXT    NOT NULL,
			pane_target       TEXT    NOT NULL DEFAULT '',
			provider          TEXT    NOT NULL,
			model             TEXT    NOT NULL,
			input_tokens      INTEGER NOT NULL DEFAULT 0,
			output_tokens     INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens    INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens  INTEGER NOT NULL DEFAULT 0,
			cost_usd          REAL    NOT NULL DEFAULT 0.0,
			project           TEXT    NOT NULL DEFAULT '',
			card_id           INTEGER DEFAULT 0,
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_project ON agent_usage(project)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_provider ON agent_usage(provider)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_created ON agent_usage(created_at)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_session ON agent_usage(session_id)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_card ON agent_usage(card_id)`)

	// Ollama persistent conversations
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS ollama_conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL DEFAULT 'New Chat',
			model TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`
		CREATE TABLE IF NOT EXISTS ollama_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id INTEGER NOT NULL REFERENCES ollama_conversations(id) ON DELETE CASCADE,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			stats TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ollama_conv_updated ON ollama_conversations(updated_at DESC)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ollama_msgs_conv ON ollama_messages(conversation_id, created_at)`)

	return nil
}

// --- Card dependencies ---

type CardDependency struct {
	ID        int64     `json:"id"`
	BlockerID int64     `json:"blockerId"`
	BlockedID int64     `json:"blockedId"`
	CreatedAt time.Time `json:"createdAt"`
}

// AddDependency records that blockerID blocks blockedID. Rejects self-loops
// and cycles (i.e. if blockedID already transitively blocks blockerID).
func (d *DB) AddDependency(blockerID, blockedID int64) error {
	_, span := telemetry.Tracer("db").Start(context.Background(), "db.AddDependency",
		telemetry.Attrs(
			attribute.Int64("workshop.card.blocker_id", blockerID),
			attribute.Int64("workshop.card.blocked_id", blockedID),
		),
	)
	defer span.End()

	if blockerID == blockedID {
		span.SetStatus(codes.Error, "self-loop")
		return fmt.Errorf("card cannot block itself")
	}
	// Cycle check: BFS from blockedID following blocker→blocked edges.
	// If we reach blockerID, adding this edge would close a cycle.
	visited := map[int64]bool{blockedID: true}
	frontier := []int64{blockedID}
	for len(frontier) > 0 {
		var next []int64
		for _, cur := range frontier {
			rows, err := d.db.Query(`SELECT blocked_id FROM card_dependencies WHERE blocker_id = ?`, cur)
			if err != nil {
				return fmt.Errorf("cycle check: %w", err)
			}
			for rows.Next() {
				var nb int64
				if err := rows.Scan(&nb); err != nil {
					rows.Close()
					return fmt.Errorf("cycle check scan: %w", err)
				}
				if nb == blockerID {
					rows.Close()
					span.SetStatus(codes.Error, "cycle detected")
					return fmt.Errorf("would create a circular dependency")
				}
				if !visited[nb] {
					visited[nb] = true
					next = append(next, nb)
				}
			}
			rows.Close()
		}
		frontier = next
	}
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO card_dependencies (blocker_id, blocked_id) VALUES (?, ?)`,
		blockerID, blockedID,
	)
	return err
}

// RemoveDependency removes the blocker→blocked edge.
func (d *DB) RemoveDependency(blockerID, blockedID int64) error {
	_, err := d.db.Exec(
		`DELETE FROM card_dependencies WHERE blocker_id=? AND blocked_id=?`,
		blockerID, blockedID,
	)
	return err
}

// ListDependencies returns all edges, optionally scoped to a project (both endpoints in project).
func (d *DB) ListDependencies(project string) ([]CardDependency, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if project == "" {
		rows, err = d.db.Query(`SELECT id, blocker_id, blocked_id, created_at FROM card_dependencies ORDER BY id`)
	} else {
		rows, err = d.db.Query(`
			SELECT cd.id, cd.blocker_id, cd.blocked_id, cd.created_at
			FROM card_dependencies cd
			JOIN cards a ON a.id = cd.blocker_id
			JOIN cards b ON b.id = cd.blocked_id
			WHERE a.project = ? AND b.project = ?
			ORDER BY cd.id
		`, project, project)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []CardDependency
	for rows.Next() {
		var dep CardDependency
		if err := rows.Scan(&dep.ID, &dep.BlockerID, &dep.BlockedID, &dep.CreatedAt); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

// --- Card CRUD ---

func (d *DB) ListCards(project string, includeArchived ...bool) ([]Card, error) {
	cards, _, err := d.ListCardsPaged(project, 0, 0, includeArchived...)
	return cards, err
}

// ListCardsPaged returns cards with pagination. When limit <= 0, all cards are
// returned (same as ListCards). The second return value is the total count of
// matching cards (ignoring limit/offset), useful for pagination metadata.
func (d *DB) ListCardsPaged(project string, limit, offset int, includeArchived ...bool) ([]Card, int, error) {
	args := []any{}
	var where []string
	if project != "" {
		where = append(where, `project = ?`)
		args = append(args, project)
	}
	if len(includeArchived) == 0 || !includeArchived[0] {
		where = append(where, `archived = 0`)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = ` WHERE ` + strings.Join(where, ` AND `)
	}

	// Count total matching cards.
	var total int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM cards`+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, title, description, "column", project, position, pane_target, labels, card_type, priority, parent_id, archived, created_at, updated_at FROM cards` + whereClause + ` ORDER BY "column", position, id`
	queryArgs := append([]any{}, args...)
	if limit > 0 {
		query += ` LIMIT ?`
		queryArgs = append(queryArgs, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			queryArgs = append(queryArgs, offset)
		}
	}

	rows, err := d.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var cards []Card
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.Project, &c.Position, &c.PaneTarget, &c.Labels, &c.CardType, &c.Priority, &c.ParentID, &c.Archived, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		cards = append(cards, c)
	}
	return cards, total, rows.Err()
}

func (d *DB) GetCard(id int64) (*Card, error) {
	var c Card
	err := d.db.QueryRow(
		`SELECT id, title, description, "column", project, position, pane_target, labels, card_type, priority, parent_id, archived, created_at, updated_at FROM cards WHERE id=?`, id,
	).Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.Project, &c.Position, &c.PaneTarget, &c.Labels, &c.CardType, &c.Priority, &c.ParentID, &c.Archived, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) CreateCard(c *Card) error {
	telemetry.KanbanMutationsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(attribute.String("op", "create")),
	)
	// Set position to end of column
	var maxPos int
	if err := d.db.QueryRow(`SELECT COALESCE(MAX(position), -1) FROM cards WHERE "column" = ? AND project = ?`, c.Column, c.Project).Scan(&maxPos); err != nil {
		maxPos = -1
	}
	c.Position = maxPos + 1

	result, err := d.db.Exec(
		`INSERT INTO cards (title, description, "column", project, position, pane_target, labels, card_type, priority, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Title, c.Description, c.Column, c.Project, c.Position, c.PaneTarget, c.Labels, c.CardType, c.Priority, c.ParentID,
	)
	if err != nil {
		return err
	}
	c.ID, _ = result.LastInsertId()
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	d.LogCardEvent(c.ID, "created", "", c.Title, "user")
	return nil
}

func (d *DB) UpdateCard(c *Card) error {
	telemetry.KanbanMutationsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(attribute.String("op", "update")),
	)
	// Fetch the old card to diff for the log
	var old Card
	d.db.QueryRow(
		`SELECT title, description, "column", priority, card_type FROM cards WHERE id=?`, c.ID,
	).Scan(&old.Title, &old.Description, &old.Column, &old.Priority, &old.CardType)

	_, err := d.db.Exec(
		`UPDATE cards SET title=?, description=?, "column"=?, project=?, position=?, pane_target=?, labels=?, card_type=?, priority=?, parent_id=?, archived=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Title, c.Description, c.Column, c.Project, c.Position, c.PaneTarget, c.Labels, c.CardType, c.Priority, c.ParentID, c.Archived, c.ID,
	)
	if err != nil {
		return err
	}

	// Log significant field changes
	if old.Title != c.Title {
		d.LogCardEvent(c.ID, "title_changed", old.Title, c.Title, "user")
	}
	if old.Column != c.Column {
		d.LogCardEvent(c.ID, "moved", old.Column, c.Column, "user")
	}
	if old.Priority != c.Priority {
		d.LogCardEvent(c.ID, "priority_changed", old.Priority, c.Priority, "user")
	}
	if old.CardType != c.CardType {
		d.LogCardEvent(c.ID, "type_changed", old.CardType, c.CardType, "user")
	}
	if old.Description != c.Description {
		d.LogCardEvent(c.ID, "description_changed", "", "", "user")
	}
	return nil
}

func (d *DB) MoveCard(id int64, toColumn string, toPosition int) error {
	_, span := telemetry.Tracer("db").Start(context.Background(), "db.MoveCard",
		telemetry.Attrs(
			attribute.Int64("workshop.card.id", id),
			attribute.String("workshop.card.to_column", toColumn),
			attribute.Int("workshop.card.to_position", toPosition),
		),
	)
	defer span.End()
	telemetry.KanbanMutationsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(attribute.String("op", "move")),
	)

	tx, err := d.db.Begin()
	if err != nil {
		span.SetStatus(codes.Error, "begin tx")
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get the card's project, column, and description for transition + gate validation
	var project, oldColumn, description string
	if err := tx.QueryRow(`SELECT project, "column", description FROM cards WHERE id = ?`, id).Scan(&project, &oldColumn, &description); err != nil {
		return fmt.Errorf("lookup card project: %w", err)
	}

	// Validate transition against the project's workflow
	if err := d.ValidateTransition(project, oldColumn, toColumn); err != nil {
		span.SetStatus(codes.Error, "invalid transition")
		return err
	}

	// Validate refinement gates (e.g. require description before in_progress)
	if err := d.ValidateGates(project, &Card{Description: description}, oldColumn, toColumn); err != nil {
		span.SetStatus(codes.Error, "gate validation failed")
		return err
	}

	// Pull the current ordered ids in the destination column (excluding the moving card),
	// splice the moving card in at toPosition, and rewrite positions 0..N-1. This keeps
	// DB position tightly aligned with render index so drop-in-place stays a true no-op.
	rows, err := tx.Query(
		`SELECT id FROM cards WHERE "column" = ? AND project = ? AND id != ? AND parent_id = 0 ORDER BY position, id`,
		toColumn, project, id,
	)
	if err != nil {
		return fmt.Errorf("list destination column: %w", err)
	}
	var destIDs []int64
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err != nil {
			rows.Close()
			return fmt.Errorf("scan dest id: %w", err)
		}
		destIDs = append(destIDs, rid)
	}
	rows.Close()
	if toPosition < 0 {
		toPosition = 0
	}
	if toPosition > len(destIDs) {
		toPosition = len(destIDs)
	}
	final := make([]int64, 0, len(destIDs)+1)
	final = append(final, destIDs[:toPosition]...)
	final = append(final, id)
	final = append(final, destIDs[toPosition:]...)

	for idx, cid := range final {
		if _, err := tx.Exec(
			`UPDATE cards SET "column"=?, position=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			toColumn, idx, cid,
		); err != nil {
			return fmt.Errorf("write dest positions: %w", err)
		}
	}
	// Auto-archive when moving to done, unarchive when moving out
	if oldColumn != toColumn {
		archiveVal := 0
		if toColumn == "done" {
			archiveVal = 1
		}
		if _, err := tx.Exec(`UPDATE cards SET archived=? WHERE id=?`, archiveVal, id); err != nil {
			return fmt.Errorf("set archived: %w", err)
		}
	}

	// If the card moved columns, re-densify the source column too so its positions stay tight.
	if oldColumn != toColumn {
		srcRows, err := tx.Query(
			`SELECT id FROM cards WHERE "column" = ? AND project = ? AND parent_id = 0 ORDER BY position, id`,
			oldColumn, project,
		)
		if err != nil {
			return fmt.Errorf("list source column: %w", err)
		}
		var srcIDs []int64
		for srcRows.Next() {
			var rid int64
			if err := srcRows.Scan(&rid); err != nil {
				srcRows.Close()
				return fmt.Errorf("scan src id: %w", err)
			}
			srcIDs = append(srcIDs, rid)
		}
		srcRows.Close()
		for idx, cid := range srcIDs {
			if _, err := tx.Exec(`UPDATE cards SET position=? WHERE id=?`, idx, cid); err != nil {
				return fmt.Errorf("write src positions: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	crossColumn := oldColumn != toColumn
	if crossColumn {
		d.LogCardEvent(id, "moved", oldColumn, toColumn, "user")
	}
	span.SetAttributes(
		attribute.String("workshop.card.from_column", oldColumn),
		attribute.Bool("workshop.card.cross_column", crossColumn),
		attribute.Int("workshop.card.dest_size", len(final)),
	)
	return nil
}

func (d *DB) DeleteCard(id int64) error {
	telemetry.KanbanMutationsTotal.Add(context.Background(), 1,
		telemetry.MetricAttrs(attribute.String("op", "delete")),
	)
	d.LogCardEvent(id, "deleted", "", "", "user")
	_, err := d.db.Exec(`DELETE FROM cards WHERE id=?`, id)
	return err
}

func (d *DB) ListProjects() ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT project FROM cards WHERE project != '' ORDER BY project`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return projects, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// --- Card Activity Log ---

type CardLogEntry struct {
	ID          int64  `json:"id"`
	CardID      int64  `json:"cardId"`
	Action      string `json:"action"`      // moved, updated, created, deleted, note_added, etc.
	BeforeValue string `json:"beforeValue"` // optional, for diff display
	AfterValue  string `json:"afterValue"`
	Source      string `json:"source"`      // "user", "agent", "system"
	CreatedAt   string `json:"createdAt"`
}

func (d *DB) LogCardEvent(cardID int64, action, before, after, source string) {
	if source == "" {
		source = "user"
	}
	d.db.Exec(
		`INSERT INTO card_log (card_id, action, before_value, after_value, source) VALUES (?, ?, ?, ?, ?)`,
		cardID, action, before, after, source,
	)
}

func (d *DB) ListCardLog(cardID int64) ([]CardLogEntry, error) {
	rows, err := d.db.Query(
		`SELECT id, card_id, action, before_value, after_value, source, created_at FROM card_log WHERE card_id=? ORDER BY created_at DESC`, cardID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []CardLogEntry
	for rows.Next() {
		var e CardLogEntry
		if err := rows.Scan(&e.ID, &e.CardID, &e.Action, &e.BeforeValue, &e.AfterValue, &e.Source, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListProjectLog returns recent activity across all cards in a project (or all if empty).
func (d *DB) ListProjectLog(project string, limit int) ([]CardLogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	var query string
	var args []any
	if project != "" {
		query = `SELECT l.id, l.card_id, l.action, l.before_value, l.after_value, l.source, l.created_at
			FROM card_log l JOIN cards c ON l.card_id = c.id
			WHERE c.project = ? ORDER BY l.created_at DESC LIMIT ?`
		args = []any{project, limit}
	} else {
		query = `SELECT id, card_id, action, before_value, after_value, source, created_at
			FROM card_log ORDER BY created_at DESC LIMIT ?`
		args = []any{limit}
	}
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []CardLogEntry
	for rows.Next() {
		var e CardLogEntry
		if err := rows.Scan(&e.ID, &e.CardID, &e.Action, &e.BeforeValue, &e.AfterValue, &e.Source, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Card Notes ---

type CardNote struct {
	ID        int64  `json:"id"`
	CardID    int64  `json:"cardId"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

func (d *DB) AddNote(cardID int64, text string) (*CardNote, error) {
	result, err := d.db.Exec(`INSERT INTO card_notes (card_id, text) VALUES (?, ?)`, cardID, text)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	d.LogCardEvent(cardID, "note_added", "", text, "user")
	return &CardNote{ID: id, CardID: cardID, Text: text}, nil
}

func (d *DB) ListNotes(cardID int64) ([]CardNote, error) {
	rows, err := d.db.Query(`SELECT id, card_id, text, created_at FROM card_notes WHERE card_id=? ORDER BY created_at`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notes []CardNote
	for rows.Next() {
		var n CardNote
		if err := rows.Scan(&n.ID, &n.CardID, &n.Text, &n.CreatedAt); err != nil {
			return notes, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// --- Card Messages ---

type CardMessage struct {
	ID        int64  `json:"id"`
	CardID    int64  `json:"cardId"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

func (d *DB) AddMessage(cardID int64, author, text string) (*CardMessage, error) {
	result, err := d.db.Exec(`INSERT INTO card_messages (card_id, author, text) VALUES (?, ?, ?)`, cardID, author, text)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &CardMessage{ID: id, CardID: cardID, Author: author, Text: text}, nil
}

func (d *DB) ListMessages(cardID int64) ([]CardMessage, error) {
	rows, err := d.db.Query(`SELECT id, card_id, author, text, created_at FROM card_messages WHERE card_id=? ORDER BY created_at`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []CardMessage
	for rows.Next() {
		var m CardMessage
		if err := rows.Scan(&m.ID, &m.CardID, &m.Author, &m.Text, &m.CreatedAt); err != nil {
			return msgs, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// --- Recordings ---

type Recording struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Target     string `json:"target"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	StartedAt  string `json:"startedAt"`
	DurationMs int64  `json:"durationMs"`
	Status     string `json:"status"` // recording, stopped
}

type RecordingFrame struct {
	OffsetMs int    `json:"offsetMs"`
	Data     string `json:"data"`
}

func (d *DB) CreateRecording(name, target string, cols, rows int) (int64, error) {
	result, err := d.db.Exec(
		`INSERT INTO recordings (name, target, cols, rows) VALUES (?, ?, ?, ?)`,
		name, target, cols, rows,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *DB) AppendFrame(recordingID int64, offsetMs int, data []byte) error {
	_, err := d.db.Exec(
		`INSERT INTO recording_frames (recording_id, offset_ms, data) VALUES (?, ?, ?)`,
		recordingID, offsetMs, data,
	)
	return err
}

func (d *DB) StopRecording(id int64, durationMs int64) error {
	_, err := d.db.Exec(
		`UPDATE recordings SET status='stopped', duration_ms=? WHERE id=?`,
		durationMs, id,
	)
	return err
}

func (d *DB) ListRecordings() ([]Recording, error) {
	rows, err := d.db.Query(`SELECT id, name, target, cols, rows, started_at, duration_ms, status FROM recordings ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recs []Recording
	for rows.Next() {
		var r Recording
		if err := rows.Scan(&r.ID, &r.Name, &r.Target, &r.Cols, &r.Rows, &r.StartedAt, &r.DurationMs, &r.Status); err != nil {
			return recs, err
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

func (d *DB) GetRecording(id int64) (*Recording, error) {
	var r Recording
	err := d.db.QueryRow(
		`SELECT id, name, target, cols, rows, started_at, duration_ms, status FROM recordings WHERE id=?`, id,
	).Scan(&r.ID, &r.Name, &r.Target, &r.Cols, &r.Rows, &r.StartedAt, &r.DurationMs, &r.Status)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (d *DB) GetRecordingFrames(id int64) ([]RecordingFrame, error) {
	rows, err := d.db.Query(`SELECT offset_ms, data FROM recording_frames WHERE recording_id=? ORDER BY offset_ms`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var frames []RecordingFrame
	for rows.Next() {
		var f RecordingFrame
		var data []byte
		if err := rows.Scan(&f.OffsetMs, &data); err != nil {
			return frames, err
		}
		f.Data = string(data)
		frames = append(frames, f)
	}
	return frames, rows.Err()
}

func (d *DB) DeleteRecording(id int64) error {
	d.db.Exec(`DELETE FROM recording_frames WHERE recording_id=?`, id)
	_, err := d.db.Exec(`DELETE FROM recordings WHERE id=?`, id)
	return err
}

// --- Consensus Runs ---

type ConsensusRun struct {
	ID              string           `json:"id"`
	Prompt          string           `json:"prompt"`
	Directory       string           `json:"directory"`
	Status          string           `json:"status"`
	CoordinatorPane string           `json:"coordinatorPane"`
	CreatedAt       string           `json:"createdAt"`
	Agents          []ConsensusAgent `json:"agentOutputs"`
}

type ConsensusAgent struct {
	ID          int64  `json:"dbId,omitempty"`
	RunID       string `json:"-"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	Target      string `json:"target"`
	Status      string `json:"status"`
	Output      string `json:"output"`
	NeedsInput  bool   `json:"needsInput"`
	InputPrompt string `json:"inputPrompt"`
}

func (d *DB) CreateConsensusRun(id, prompt, directory string) error {
	_, err := d.db.Exec(
		`INSERT INTO consensus_runs (id, prompt, directory) VALUES (?, ?, ?)`,
		id, prompt, directory,
	)
	return err
}

func (d *DB) CreateConsensusAgent(runID, name, model, provider, target, status string) error {
	_, err := d.db.Exec(
		`INSERT INTO consensus_agents (run_id, name, model, provider, target, status) VALUES (?, ?, ?, ?, ?, ?)`,
		runID, name, model, provider, target, status,
	)
	return err
}

func (d *DB) UpdateConsensusRunStatus(id, status string) error {
	_, err := d.db.Exec(
		`UPDATE consensus_runs SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		status, id,
	)
	return err
}

func (d *DB) UpdateConsensusRunCoordinator(id, coordinatorPane string) error {
	_, err := d.db.Exec(
		`UPDATE consensus_runs SET coordinator_pane=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		coordinatorPane, id,
	)
	return err
}

func (d *DB) UpdateConsensusAgent(runID, name, status, output string, needsInput bool, inputPrompt string) error {
	ni := 0
	if needsInput {
		ni = 1
	}
	_, err := d.db.Exec(
		`UPDATE consensus_agents SET status=?, output=?, needs_input=?, input_prompt=? WHERE run_id=? AND name=?`,
		status, output, ni, inputPrompt, runID, name,
	)
	return err
}

func (d *DB) GetConsensusRun(id string) (*ConsensusRun, error) {
	var run ConsensusRun
	err := d.db.QueryRow(
		`SELECT id, prompt, directory, status, coordinator_pane, created_at FROM consensus_runs WHERE id=?`, id,
	).Scan(&run.ID, &run.Prompt, &run.Directory, &run.Status, &run.CoordinatorPane, &run.CreatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := d.db.Query(
		`SELECT name, model, provider, target, status, output, needs_input, input_prompt FROM consensus_agents WHERE run_id=?`, id,
	)
	if err != nil {
		return &run, nil
	}
	defer rows.Close()

	for rows.Next() {
		var a ConsensusAgent
		var ni int
		if err := rows.Scan(&a.Name, &a.Model, &a.Provider, &a.Target, &a.Status, &a.Output, &ni, &a.InputPrompt); err != nil {
			continue
		}
		a.NeedsInput = ni != 0
		run.Agents = append(run.Agents, a)
	}
	return &run, nil
}

func (d *DB) ListConsensusRuns() ([]ConsensusRun, error) {
	rows, err := d.db.Query(`SELECT id, prompt, directory, status, coordinator_pane, created_at FROM consensus_runs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []ConsensusRun
	for rows.Next() {
		var r ConsensusRun
		if err := rows.Scan(&r.ID, &r.Prompt, &r.Directory, &r.Status, &r.CoordinatorPane, &r.CreatedAt); err != nil {
			continue
		}
		runs = append(runs, r)
	}

	// Load agents for each run
	for i := range runs {
		agentRows, err := d.db.Query(
			`SELECT name, model, provider, target, status, output, needs_input, input_prompt FROM consensus_agents WHERE run_id=?`, runs[i].ID,
		)
		if err != nil {
			continue
		}
		for agentRows.Next() {
			var a ConsensusAgent
			var ni int
			if err := agentRows.Scan(&a.Name, &a.Model, &a.Provider, &a.Target, &a.Status, &a.Output, &ni, &a.InputPrompt); err != nil {
				continue
			}
			a.NeedsInput = ni != 0
			runs[i].Agents = append(runs[i].Agents, a)
		}
		agentRows.Close()
	}
	return runs, nil
}

// --- Card Dispatches ---

type Dispatch struct {
	ID           int64      `json:"id"`
	CardID       int64      `json:"cardId"`
	SessionName  string     `json:"sessionName"`
	Target       string     `json:"target"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status"` // running, done, error
	AutoCleanup  bool       `json:"autoCleanup"`
	WorktreeDir  string     `json:"worktreeDir,omitempty"`
	Branch       string     `json:"branch,omitempty"`
	DispatchedAt time.Time  `json:"dispatchedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

func (d *DB) CreateDispatch(cardID int64, sessionName, target, provider string, autoCleanup bool, worktreeDir, branch string) (*Dispatch, error) {
	cleanup := 0
	if autoCleanup {
		cleanup = 1
	}
	result, err := d.db.Exec(
		`INSERT INTO card_dispatches (card_id, session_name, target, provider, auto_cleanup, worktree_dir, branch) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cardID, sessionName, target, provider, cleanup, worktreeDir, branch,
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &Dispatch{
		ID: id, CardID: cardID, SessionName: sessionName, Target: target,
		Provider: provider, Status: "running", AutoCleanup: autoCleanup,
		WorktreeDir: worktreeDir, Branch: branch,
		DispatchedAt: time.Now(),
	}, nil
}

func (d *DB) ListDispatches(cardID int64) ([]Dispatch, error) {
	rows, err := d.db.Query(
		`SELECT id, card_id, session_name, target, provider, status, auto_cleanup, worktree_dir, branch, dispatched_at, completed_at
		 FROM card_dispatches WHERE card_id=? ORDER BY dispatched_at DESC`, cardID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dispatches []Dispatch
	for rows.Next() {
		var disp Dispatch
		var cleanup int
		if err := rows.Scan(&disp.ID, &disp.CardID, &disp.SessionName, &disp.Target, &disp.Provider, &disp.Status, &cleanup, &disp.WorktreeDir, &disp.Branch, &disp.DispatchedAt, &disp.CompletedAt); err != nil {
			continue
		}
		disp.AutoCleanup = cleanup != 0
		dispatches = append(dispatches, disp)
	}
	return dispatches, rows.Err()
}

func (d *DB) GetActiveDispatches() ([]Dispatch, error) {
	rows, err := d.db.Query(
		`SELECT id, card_id, session_name, target, provider, status, auto_cleanup, worktree_dir, branch, dispatched_at, completed_at
		 FROM card_dispatches WHERE status='running' ORDER BY dispatched_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dispatches []Dispatch
	for rows.Next() {
		var disp Dispatch
		var cleanup int
		if err := rows.Scan(&disp.ID, &disp.CardID, &disp.SessionName, &disp.Target, &disp.Provider, &disp.Status, &cleanup, &disp.WorktreeDir, &disp.Branch, &disp.DispatchedAt, &disp.CompletedAt); err != nil {
			continue
		}
		disp.AutoCleanup = cleanup != 0
		dispatches = append(dispatches, disp)
	}
	return dispatches, rows.Err()
}

func (d *DB) CompleteDispatch(id int64, status string) error {
	_, err := d.db.Exec(
		`UPDATE card_dispatches SET status=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
		status, id,
	)
	return err
}

// --- Channels ---

type Channel struct {
	Name            string `json:"name"`
	Project         string `json:"project,omitempty"`
	SubscriberCount int    `json:"subscriberCount"`
	MessageCount    int    `json:"messageCount"`
}

type ChannelMessageRecord struct {
	ID        int64     `json:"id"`
	Channel   string    `json:"channel"`
	Sender    string    `json:"sender"`
	Body      string    `json:"body"`
	Project   string    `json:"project,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateChannelSubscription registers a pane as a subscriber. Idempotent.
func (d *DB) CreateChannelSubscription(channel, target, project string) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO channel_subscriptions (channel, target, project) VALUES (?, ?, ?)`,
		channel, target, project,
	)
	return err
}

// DeleteChannelSubscription removes a pane from a channel.
func (d *DB) DeleteChannelSubscription(channel, target string) error {
	_, err := d.db.Exec(
		`DELETE FROM channel_subscriptions WHERE channel=? AND target=?`,
		channel, target,
	)
	return err
}

// ListChannelSubscribers returns the pane targets subscribed to a channel.
func (d *DB) ListChannelSubscribers(channel string) ([]string, error) {
	rows, err := d.db.Query(
		`SELECT target FROM channel_subscriptions WHERE channel=? ORDER BY created_at ASC`,
		channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// ListChannels returns all active channels (those with at least one
// subscriber or message), optionally filtered by project.
func (d *DB) ListChannels(project string) ([]Channel, error) {
	query := `
		SELECT
			COALESCE(s.channel, m.channel) AS name,
			COALESCE(s.project, m.project, '') AS project,
			COALESCE(s.sub_count, 0) AS sub_count,
			COALESCE(m.msg_count, 0) AS msg_count
		FROM
			(SELECT channel, project, COUNT(*) AS sub_count FROM channel_subscriptions GROUP BY channel) s
			FULL OUTER JOIN
			(SELECT channel, project, COUNT(*) AS msg_count FROM channel_messages GROUP BY channel) m
			ON s.channel = m.channel
	`
	// SQLite doesn't support FULL OUTER JOIN — use UNION of two LEFT JOINs.
	query = `
		SELECT channel, project, sub_count, msg_count FROM (
			SELECT s.channel AS channel,
				COALESCE(s.project, '') AS project,
				s.sub_count,
				COALESCE(m.msg_count, 0) AS msg_count
			FROM (SELECT channel, MAX(project) AS project, COUNT(*) AS sub_count FROM channel_subscriptions GROUP BY channel) s
			LEFT JOIN (SELECT channel, COUNT(*) AS msg_count FROM channel_messages GROUP BY channel) m ON s.channel = m.channel
			UNION
			SELECT m.channel AS channel,
				COALESCE(m.project, '') AS project,
				COALESCE(s.sub_count, 0) AS sub_count,
				m.msg_count
			FROM (SELECT channel, MAX(project) AS project, COUNT(*) AS msg_count FROM channel_messages GROUP BY channel) m
			LEFT JOIN (SELECT channel, COUNT(*) AS sub_count FROM channel_subscriptions GROUP BY channel) s ON m.channel = s.channel
		)
	`
	var args []any
	if project != "" {
		query += ` WHERE project = ? OR project = ''`
		args = append(args, project)
	}
	query += ` ORDER BY channel ASC`
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	seen := map[string]bool{}
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.Name, &c.Project, &c.SubscriberCount, &c.MessageCount); err != nil {
			continue
		}
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out, nil
}

// CreateChannelMessage persists a published message.
func (d *DB) CreateChannelMessage(channel, sender, body, project string) (*ChannelMessageRecord, error) {
	res, err := d.db.Exec(
		`INSERT INTO channel_messages (channel, sender, body, project) VALUES (?, ?, ?, ?)`,
		channel, sender, body, project,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ChannelMessageRecord{
		ID:        id,
		Channel:   channel,
		Sender:    sender,
		Body:      body,
		Project:   project,
		CreatedAt: time.Now(),
	}, nil
}

// ListChannelMessages returns the most recent messages on a channel.
func (d *DB) ListChannelMessages(channel string, limit int) ([]ChannelMessageRecord, error) {
	rows, err := d.db.Query(
		`SELECT id, channel, sender, body, project, created_at
		 FROM channel_messages WHERE channel=?
		 ORDER BY created_at DESC LIMIT ?`,
		channel, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelMessageRecord
	for rows.Next() {
		var m ChannelMessageRecord
		if err := rows.Scan(&m.ID, &m.Channel, &m.Sender, &m.Body, &m.Project, &m.CreatedAt); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// --- Workflows ---

// GetWorkflow returns the workflow config for a project, or nil if none is set.
func (d *DB) GetWorkflow(project string) (*WorkflowConfig, error) {
	var configJSON string
	err := d.db.QueryRow(`SELECT config FROM workflows WHERE project = ?`, project).Scan(&configJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	var wf WorkflowConfig
	if err := json.Unmarshal([]byte(configJSON), &wf); err != nil {
		return nil, fmt.Errorf("parse workflow config: %w", err)
	}
	return &wf, nil
}

// GetOrDefaultWorkflow returns the project's workflow, falling back to DefaultWorkflow.
func (d *DB) GetOrDefaultWorkflow(project string) (*WorkflowConfig, error) {
	wf, err := d.GetWorkflow(project)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		def := DefaultWorkflow
		return &def, nil
	}
	return wf, nil
}

// SetWorkflow upserts the workflow config for a project.
func (d *DB) SetWorkflow(project string, wf *WorkflowConfig) error {
	data, err := json.Marshal(wf)
	if err != nil {
		return fmt.Errorf("marshal workflow: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT INTO workflows (project, config) VALUES (?, ?)
		 ON CONFLICT(project) DO UPDATE SET config=excluded.config, updated_at=CURRENT_TIMESTAMP`,
		project, string(data),
	)
	return err
}

// ValidateTransition checks whether moving from one column to another is allowed
// by the project's workflow. Returns nil if allowed, an error describing the violation otherwise.
func (d *DB) ValidateTransition(project, fromCol, toCol string) error {
	if fromCol == toCol {
		return nil // reorder within same column is always OK
	}
	wf, err := d.GetOrDefaultWorkflow(project)
	if err != nil {
		return fmt.Errorf("load workflow: %w", err)
	}
	allowed, ok := wf.Transitions[fromCol]
	if !ok {
		return fmt.Errorf("unknown source column %q in workflow", fromCol)
	}
	for _, col := range allowed {
		if col == toCol {
			return nil
		}
	}
	return fmt.Errorf("transition %s → %s not allowed (valid: %s)", fromCol, toCol, strings.Join(allowed, ", "))
}

// ValidateGates checks whether a card satisfies the refinement gate for a
// specific transition. Returns nil if no gate exists or the card passes.
func (d *DB) ValidateGates(project string, card *Card, fromCol, toCol string) error {
	if fromCol == toCol {
		return nil
	}
	wf, err := d.GetOrDefaultWorkflow(project)
	if err != nil {
		return fmt.Errorf("load workflow: %w", err)
	}
	if wf.Gates == nil {
		return nil
	}
	key := fromCol + "→" + toCol
	gate, ok := wf.Gates[key]
	if !ok {
		return nil
	}
	desc := strings.TrimSpace(card.Description)
	if gate.RequireDescription && desc == "" {
		return fmt.Errorf("gate %s: card requires a description before moving to %s", key, toCol)
	}
	if gate.RequireChecklist && !strings.Contains(desc, "- [ ]") && !strings.Contains(desc, "- [x]") {
		return fmt.Errorf("gate %s: card requires at least one checklist item (- [ ]) before moving to %s", key, toCol)
	}
	return nil
}

// seedDefaultWorkflows inserts the default workflow for every project in the
// cards table that doesn't already have a workflow row. Called during migrate()
// so existing databases get seeded on upgrade; idempotent on subsequent runs.
func (d *DB) seedDefaultWorkflows() {
	rows, err := d.db.Query(
		`SELECT DISTINCT c.project FROM cards c
		 WHERE c.project != ''
		   AND c.project NOT IN (SELECT w.project FROM workflows w)`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	defaultJSON, _ := json.Marshal(DefaultWorkflow)
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			continue
		}
		d.db.Exec(
			`INSERT OR IGNORE INTO workflows (project, config) VALUES (?, ?)`,
			project, string(defaultJSON),
		)
	}
}

// --- Activity Log ---

type ActivityEntry struct {
	ID         int64            `json:"id"`
	ParentID   int64            `json:"parentId"`
	PaneTarget string           `json:"paneTarget"`
	AgentName  string           `json:"agentName"`
	ActionType string           `json:"actionType"` // file_write, command, decision, error, status, etc.
	Summary    string           `json:"summary"`
	Metadata   string           `json:"metadata"` // JSON blob for structured data
	Project    string           `json:"project"`
	CreatedAt  string           `json:"createdAt"`
	Children   []ActivityEntry  `json:"children,omitempty"` // populated in tree mode
}

// RecordActivity inserts an activity log entry and returns its ID.
func (d *DB) RecordActivity(entry *ActivityEntry) (int64, error) {
	if entry.Metadata == "" {
		entry.Metadata = "{}"
	}
	res, err := d.db.Exec(
		`INSERT INTO activity_log (pane_target, agent_name, action_type, summary, metadata, project, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.PaneTarget, entry.AgentName, entry.ActionType, entry.Summary, entry.Metadata, entry.Project, entry.ParentID,
	)
	if err != nil {
		return 0, fmt.Errorf("record activity: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListActivity returns recent activity entries, optionally filtered by pane, project, or action type.
// When tree is true, entries are returned as a forest: root entries (parent_id=0) with children nested.
func (d *DB) ListActivity(pane, project, actionType string, limit int, tree bool) ([]ActivityEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	var clauses []string
	var args []any
	if pane != "" {
		clauses = append(clauses, "pane_target = ?")
		args = append(args, pane)
	}
	if project != "" {
		clauses = append(clauses, "project = ?")
		args = append(args, project)
	}
	if actionType != "" {
		clauses = append(clauses, "action_type = ?")
		args = append(args, actionType)
	}

	query := "SELECT id, parent_id, pane_target, agent_name, action_type, summary, metadata, project, created_at FROM activity_log"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ActivityEntry
	for rows.Next() {
		var e ActivityEntry
		if err := rows.Scan(&e.ID, &e.ParentID, &e.PaneTarget, &e.AgentName, &e.ActionType, &e.Summary, &e.Metadata, &e.Project, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if !tree {
		return entries, nil
	}

	// Build tree: two-pass — first attach children, then collect roots.
	// Must be two passes because parents may appear before children in DESC order.
	byID := make(map[int64]*ActivityEntry, len(entries))
	for i := range entries {
		byID[entries[i].ID] = &entries[i]
	}
	childIDs := make(map[int64]bool)
	for i := range entries {
		e := &entries[i]
		if e.ParentID != 0 {
			if parent, ok := byID[e.ParentID]; ok {
				parent.Children = append(parent.Children, *e)
				childIDs[e.ID] = true
			}
		}
	}
	var roots []ActivityEntry
	for i := range entries {
		if !childIDs[entries[i].ID] {
			roots = append(roots, *byID[entries[i].ID]) // copy after children are attached
		}
	}
	return roots, nil
}

// --- Approval Requests ---

type ApprovalRequest struct {
	ID         int64  `json:"id"`
	PaneTarget string `json:"paneTarget"`
	AgentName  string `json:"agentName"`
	Action     string `json:"action"`
	Details    string `json:"details"`
	Diff       string `json:"diff"`
	Project    string `json:"project"`
	Status     string `json:"status"` // pending, approved, denied
	DecidedAt  string `json:"decidedAt,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// CreateApproval inserts a pending approval request and returns its ID.
func (d *DB) CreateApproval(req *ApprovalRequest) (int64, error) {
	res, err := d.db.Exec(
		`INSERT INTO approval_requests (pane_target, agent_name, action, details, diff, project) VALUES (?, ?, ?, ?, ?, ?)`,
		req.PaneTarget, req.AgentName, req.Action, req.Details, req.Diff, req.Project,
	)
	if err != nil {
		return 0, fmt.Errorf("create approval: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ResolveApproval updates the status of a pending approval to approved or denied.
func (d *DB) ResolveApproval(id int64, decision string) error {
	if decision != "approved" && decision != "denied" {
		return fmt.Errorf("invalid decision: %s (must be approved or denied)", decision)
	}
	result, err := d.db.Exec(
		`UPDATE approval_requests SET status = ?, decided_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'pending'`,
		decision, id,
	)
	if err != nil {
		return fmt.Errorf("resolve approval: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("approval #%d not found or already resolved", id)
	}
	return nil
}

// ListApprovals returns approval requests filtered by status.
func (d *DB) ListApprovals(status string, limit int) ([]ApprovalRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	var query string
	var args []any
	if status != "" {
		query = `SELECT id, pane_target, agent_name, action, details, diff, project, status, COALESCE(decided_at, ''), created_at FROM approval_requests WHERE status = ? ORDER BY created_at DESC LIMIT ?`
		args = []any{status, limit}
	} else {
		query = `SELECT id, pane_target, agent_name, action, details, diff, project, status, COALESCE(decided_at, ''), created_at FROM approval_requests ORDER BY created_at DESC LIMIT ?`
		args = []any{limit}
	}
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reqs []ApprovalRequest
	for rows.Next() {
		var r ApprovalRequest
		if err := rows.Scan(&r.ID, &r.PaneTarget, &r.AgentName, &r.Action, &r.Details, &r.Diff, &r.Project, &r.Status, &r.DecidedAt, &r.CreatedAt); err != nil {
			continue
		}
		reqs = append(reqs, r)
	}
	return reqs, rows.Err()
}

// --- Agent Presets ---

type AgentPreset struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"systemPrompt"`
	Directory    string `json:"directory"`
}

// ListPresets returns all agent presets.
func (d *DB) ListPresets() ([]AgentPreset, error) {
	rows, err := d.db.Query(`SELECT id, name, description, provider, model, prompt, system_prompt, directory FROM agent_presets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var presets []AgentPreset
	for rows.Next() {
		var p AgentPreset
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Provider, &p.Model, &p.Prompt, &p.SystemPrompt, &p.Directory); err != nil {
			continue
		}
		presets = append(presets, p)
	}
	return presets, rows.Err()
}

// GetPreset returns a single preset by name.
func (d *DB) GetPreset(name string) (*AgentPreset, error) {
	var p AgentPreset
	err := d.db.QueryRow(
		`SELECT id, name, description, provider, model, prompt, system_prompt, directory FROM agent_presets WHERE name = ?`, name,
	).Scan(&p.ID, &p.Name, &p.Description, &p.Provider, &p.Model, &p.Prompt, &p.SystemPrompt, &p.Directory)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertPreset creates or updates a preset by name.
func (d *DB) UpsertPreset(p *AgentPreset) error {
	_, err := d.db.Exec(
		`INSERT INTO agent_presets (name, description, provider, model, prompt, system_prompt, directory)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   description=excluded.description, provider=excluded.provider, model=excluded.model,
		   prompt=excluded.prompt, system_prompt=excluded.system_prompt, directory=excluded.directory,
		   updated_at=CURRENT_TIMESTAMP`,
		p.Name, p.Description, p.Provider, p.Model, p.Prompt, p.SystemPrompt, p.Directory,
	)
	return err
}

// ensurePreset upserts a preset, updating it if it already exists.
func (d *DB) ensurePreset(p *AgentPreset) {
	d.UpsertPreset(p)
}

// DeletePreset removes a preset by name.
func (d *DB) DeletePreset(name string) error {
	_, err := d.db.Exec(`DELETE FROM agent_presets WHERE name = ?`, name)
	return err
}

// seedDefaultPresets inserts built-in presets if no presets exist yet.
func (d *DB) seedDefaultPresets() {
	var count int
	d.db.QueryRow(`SELECT COUNT(*) FROM agent_presets`).Scan(&count)
	if count > 0 {
		return
	}
	defaults := []AgentPreset{
		{
			Name:         "reviewer",
			Description:  "Code reviewer — examines diffs for bugs, style issues, and missed edge cases",
			Provider:     "claude",
			Model:        "sonnet",
			SystemPrompt: "You are a senior code reviewer. Review the code changes thoroughly. Focus on: bugs, edge cases, security issues, performance problems, and style inconsistencies. Be specific and actionable. Don't nitpick formatting unless it affects readability.",
		},
		{
			Name:         "tester",
			Description:  "Test writer — generates comprehensive test cases for code changes",
			Provider:     "claude",
			Model:        "sonnet",
			SystemPrompt: "You are a test engineer. Write comprehensive tests for the code. Cover: happy path, edge cases, error conditions, and boundary values. Use the project's existing test framework and patterns. Tests should be specific and deterministic.",
		},
		{
			Name:         "security",
			Description:  "Security engineer — audits code for vulnerabilities and OWASP issues",
			Provider:     "claude",
			Model:        "sonnet",
			SystemPrompt: "You are a security engineer. Audit the code for vulnerabilities including: injection attacks, XSS, CSRF, auth/authz issues, secrets exposure, insecure defaults, and OWASP Top 10. Rate findings by severity (critical/high/medium/low). Be specific about the attack vector and remediation.",
		},
		{
			Name:         "planner",
			Description:  "Technical planner — breaks down features into implementation tasks",
			Provider:     "claude",
			Model:        "opus",
			SystemPrompt: "You are a technical architect and planner. Break down the given feature or task into concrete implementation steps. For each step, identify: files to modify, dependencies, risks, and estimated complexity. Order steps by dependency. Flag anything that needs user input or decision.",
		},
		{
			Name:         "refactorer",
			Description:  "Refactoring specialist — improves code structure without changing behavior",
			Provider:     "claude",
			Model:        "sonnet",
			SystemPrompt: "You are a refactoring specialist. Improve code structure, readability, and maintainability without changing external behavior. Focus on: reducing duplication, improving naming, simplifying complex logic, extracting reusable components. Always verify behavior is preserved with existing tests.",
		},
		{
			Name:         "architect",
			Description:  "System architect — designs high-level solutions and evaluates trade-offs",
			Provider:     "claude",
			Model:        "opus",
			SystemPrompt: "You are a senior system architect. Design solutions that balance correctness, simplicity, performance, and maintainability. Evaluate trade-offs explicitly. Consider: scalability, failure modes, operational complexity, and team familiarity. Recommend the approach you'd bet your production on.",
		},
	}
	for _, p := range defaults {
		d.UpsertPreset(&p)
	}

}

// seedOrchestrator ensures the orchestrator preset exists even on existing databases.
func (d *DB) seedOrchestrator() {
	d.ensurePreset(&AgentPreset{
		Name:        "orchestrator",
		Description: "Task orchestrator — drives a card through plan→implement→test→review→PR phases automatically",
		Provider:    "claude",
		Model:       "opus",
		SystemPrompt: `You are a task orchestrator for the Yuna project management system. You drive a kanban card through structured phases, launching specialist agents and coordinating their work.

## Your workflow

You will be given a card ID and its description. Execute these phases in order:

### Phase 1: Plan
- Call report_activity(action="phase_start", summary="Phase: Plan — card #<id>", project=<project>)
- Call launch_agent(preset="planner", prompt="<card description + context>", directory=<dir>)
- Wait for the planner to finish (poll capture_pane every 30s until idle)
- Capture the planner's output and add it as a kanban note
- Call request_approval(action="proceed_to_implement", details="Plan complete. Review the plan above and approve to start implementation.")
- If denied, add a note and stop

### Phase 2: Implement
- Call report_activity for phase start
- Call launch_agent(preset="refactorer" or no preset for general implementation, prompt="Implement the plan from the planning phase: <plan output>", directory=<dir>)
- Wait for completion, capture output, add as note
- Call request_approval(action="proceed_to_test", details="Implementation complete.")
- If denied, stop

### Phase 3: Test
- Call launch_agent(preset="tester", prompt="Write tests for the changes just implemented: <impl summary>", directory=<dir>)
- Wait, capture, note
- Call request_approval(action="proceed_to_review", details="Tests written.")

### Phase 4: Review
- Call launch_agent(preset="reviewer", prompt="Review all changes made in this card: <summary of all phases>", directory=<dir>)
- Wait, capture, note
- Call request_approval(action="proceed_to_pr", details="Review complete. Findings: <review output>")

### Phase 5: PR
- The implementation agent should have already committed. If not, commit now.
- Move the card to review column: kanban_move(id=<id>, column="review")
- Add a final note summarizing all phases
- Call report_activity(action="status", summary="Orchestration complete for card #<id>")

## Worktree isolation
If you are running in a git worktree (the working directory will be under .worktrees/), all commits automatically land on the worktree branch — NOT main. This is intentional. Do NOT switch branches or attempt to merge to main. The user will merge/cherry-pick when ready. Pass the same working directory to all subagents you launch so they also work in the worktree.

## Rules
- NEVER call orchestrate_card — you ARE the orchestrator. Use the individual tools directly: launch_agent, capture_pane, report_activity, request_approval, kanban_add_note, kanban_move
- Use report_activity with parent_id to nest all phase activities under a root entry
- After each phase, summarize the output concisely in a kanban note
- If any phase fails or is denied, stop gracefully — add a note explaining why
- Always clean up agent sessions when done (they auto-cleanup via dispatch tracking)
- Pass context forward: each phase's prompt should include relevant output from previous phases
- When launching subagents, always pass directory=<your working directory> so they work in the same location (especially important for worktree isolation)
- When launching subagents, wait for them by polling capture_pane every 30 seconds until the agent shows an idle prompt`,
	})
}

// --- Ollama conversations ---

type OllamaConversation struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	SystemPrompt string `json:"systemPrompt"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type OllamaMessage struct {
	ID             int64  `json:"id"`
	ConversationID int64  `json:"conversationId"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	Model          string `json:"model"`
	Stats          string `json:"stats"`
	CreatedAt      string `json:"createdAt"`
}

func (d *DB) ListOllamaConversations() ([]OllamaConversation, error) {
	rows, err := d.db.Query(`SELECT id, title, model, system_prompt, created_at, updated_at FROM ollama_conversations ORDER BY updated_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var convs []OllamaConversation
	for rows.Next() {
		var c OllamaConversation
		if err := rows.Scan(&c.ID, &c.Title, &c.Model, &c.SystemPrompt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}

func (d *DB) GetOllamaConversation(id int64) (*OllamaConversation, error) {
	var c OllamaConversation
	err := d.db.QueryRow(
		`SELECT id, title, model, system_prompt, created_at, updated_at FROM ollama_conversations WHERE id=?`, id,
	).Scan(&c.ID, &c.Title, &c.Model, &c.SystemPrompt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) CreateOllamaConversation(conv *OllamaConversation) error {
	result, err := d.db.Exec(
		`INSERT INTO ollama_conversations (title, model, system_prompt) VALUES (?, ?, ?)`,
		conv.Title, conv.Model, conv.SystemPrompt,
	)
	if err != nil {
		return err
	}
	conv.ID, _ = result.LastInsertId()
	// Populate timestamps for the response
	row := d.db.QueryRow(`SELECT created_at, updated_at FROM ollama_conversations WHERE id=?`, conv.ID)
	row.Scan(&conv.CreatedAt, &conv.UpdatedAt)
	return nil
}

func (d *DB) UpdateOllamaConversation(conv *OllamaConversation) error {
	_, err := d.db.Exec(
		`UPDATE ollama_conversations SET title=?, model=?, system_prompt=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		conv.Title, conv.Model, conv.SystemPrompt, conv.ID,
	)
	return err
}

func (d *DB) DeleteOllamaConversation(id int64) error {
	_, err := d.db.Exec(`DELETE FROM ollama_conversations WHERE id=?`, id)
	return err
}

func (d *DB) ListOllamaMessages(conversationID int64) ([]OllamaMessage, error) {
	rows, err := d.db.Query(
		`SELECT id, conversation_id, role, content, model, stats, created_at FROM ollama_messages WHERE conversation_id=? ORDER BY created_at ASC`, conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []OllamaMessage
	for rows.Next() {
		var m OllamaMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Model, &m.Stats, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (d *DB) CreateOllamaMessage(msg *OllamaMessage) error {
	result, err := d.db.Exec(
		`INSERT INTO ollama_messages (conversation_id, role, content, model, stats) VALUES (?, ?, ?, ?, ?)`,
		msg.ConversationID, msg.Role, msg.Content, msg.Model, msg.Stats,
	)
	if err != nil {
		return err
	}
	msg.ID, _ = result.LastInsertId()
	// Populate created_at for the response
	d.db.QueryRow(`SELECT created_at FROM ollama_messages WHERE id=?`, msg.ID).Scan(&msg.CreatedAt)
	// Touch the parent conversation's updated_at
	d.db.Exec(`UPDATE ollama_conversations SET updated_at=CURRENT_TIMESTAMP WHERE id=?`, msg.ConversationID)
	return nil
}
