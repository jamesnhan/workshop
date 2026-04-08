package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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

	// Indexes for hot query paths
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cards_proj_col ON cards(project, "column", position)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notes_card ON card_notes(card_id, created_at)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rec_frames ON recording_frames(recording_id, offset_ms)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_consensus_agents_run ON consensus_agents(run_id)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_card_log_card ON card_log(card_id, created_at DESC)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cards_parent ON cards(parent_id)`)

	return nil
}

// --- Card CRUD ---

func (d *DB) ListCards(project string) ([]Card, error) {
	query := `SELECT id, title, description, "column", project, position, pane_target, labels, card_type, priority, parent_id, created_at, updated_at FROM cards`
	args := []any{}
	if project != "" {
		query += ` WHERE project = ?`
		args = append(args, project)
	}
	query += ` ORDER BY "column", position, id`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []Card
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.Project, &c.Position, &c.PaneTarget, &c.Labels, &c.CardType, &c.Priority, &c.ParentID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

func (d *DB) GetCard(id int64) (*Card, error) {
	var c Card
	err := d.db.QueryRow(
		`SELECT id, title, description, "column", project, position, pane_target, labels, card_type, priority, parent_id, created_at, updated_at FROM cards WHERE id=?`, id,
	).Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.Project, &c.Position, &c.PaneTarget, &c.Labels, &c.CardType, &c.Priority, &c.ParentID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) CreateCard(c *Card) error {
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
	// Fetch the old card to diff for the log
	var old Card
	d.db.QueryRow(
		`SELECT title, description, "column", priority, card_type FROM cards WHERE id=?`, c.ID,
	).Scan(&old.Title, &old.Description, &old.Column, &old.Priority, &old.CardType)

	_, err := d.db.Exec(
		`UPDATE cards SET title=?, description=?, "column"=?, project=?, position=?, pane_target=?, labels=?, card_type=?, priority=?, parent_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Title, c.Description, c.Column, c.Project, c.Position, c.PaneTarget, c.Labels, c.CardType, c.Priority, c.ParentID, c.ID,
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
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get the card's project and current column for the shift + log
	var project, oldColumn string
	if err := tx.QueryRow(`SELECT project, "column" FROM cards WHERE id = ?`, id).Scan(&project, &oldColumn); err != nil {
		return fmt.Errorf("lookup card project: %w", err)
	}

	// Shift cards at or after the target position within the same project
	if _, err := tx.Exec(
		`UPDATE cards SET position = position + 1 WHERE "column" = ? AND project = ? AND position >= ? AND id != ?`,
		toColumn, project, toPosition, id,
	); err != nil {
		return fmt.Errorf("shift positions: %w", err)
	}

	// Move the card
	if _, err := tx.Exec(
		`UPDATE cards SET "column"=?, position=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		toColumn, toPosition, id,
	); err != nil {
		return fmt.Errorf("move card: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if oldColumn != toColumn {
		d.LogCardEvent(id, "moved", oldColumn, toColumn, "user")
	}
	return nil
}

func (d *DB) DeleteCard(id int64) error {
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
	DispatchedAt time.Time  `json:"dispatchedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

func (d *DB) CreateDispatch(cardID int64, sessionName, target, provider string, autoCleanup bool) (*Dispatch, error) {
	cleanup := 0
	if autoCleanup {
		cleanup = 1
	}
	result, err := d.db.Exec(
		`INSERT INTO card_dispatches (card_id, session_name, target, provider, auto_cleanup) VALUES (?, ?, ?, ?, ?)`,
		cardID, sessionName, target, provider, cleanup,
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &Dispatch{
		ID: id, CardID: cardID, SessionName: sessionName, Target: target,
		Provider: provider, Status: "running", AutoCleanup: autoCleanup,
		DispatchedAt: time.Now(),
	}, nil
}

func (d *DB) ListDispatches(cardID int64) ([]Dispatch, error) {
	rows, err := d.db.Query(
		`SELECT id, card_id, session_name, target, provider, status, auto_cleanup, dispatched_at, completed_at
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
		if err := rows.Scan(&disp.ID, &disp.CardID, &disp.SessionName, &disp.Target, &disp.Provider, &disp.Status, &cleanup, &disp.DispatchedAt, &disp.CompletedAt); err != nil {
			continue
		}
		disp.AutoCleanup = cleanup != 0
		dispatches = append(dispatches, disp)
	}
	return dispatches, rows.Err()
}

func (d *DB) GetActiveDispatches() ([]Dispatch, error) {
	rows, err := d.db.Query(
		`SELECT id, card_id, session_name, target, provider, status, auto_cleanup, dispatched_at, completed_at
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
		if err := rows.Scan(&disp.ID, &disp.CardID, &disp.SessionName, &disp.Target, &disp.Provider, &disp.Status, &cleanup, &disp.DispatchedAt, &disp.CompletedAt); err != nil {
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
