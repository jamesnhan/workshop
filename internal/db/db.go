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

	// Indexes for hot query paths
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_cards_proj_col ON cards(project, "column", position)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_notes_card ON card_notes(card_id, created_at)`)
	d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rec_frames ON recording_frames(recording_id, offset_ms)`)

	return nil
}

// --- Card CRUD ---

func (d *DB) ListCards(project string) ([]Card, error) {
	query := `SELECT id, title, description, "column", project, position, pane_target, labels, card_type, priority, created_at, updated_at FROM cards`
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
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.Column, &c.Project, &c.Position, &c.PaneTarget, &c.Labels, &c.CardType, &c.Priority, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

func (d *DB) CreateCard(c *Card) error {
	// Set position to end of column
	var maxPos int
	if err := d.db.QueryRow(`SELECT COALESCE(MAX(position), -1) FROM cards WHERE "column" = ? AND project = ?`, c.Column, c.Project).Scan(&maxPos); err != nil {
		maxPos = -1
	}
	c.Position = maxPos + 1

	result, err := d.db.Exec(
		`INSERT INTO cards (title, description, "column", project, position, pane_target, labels, card_type, priority) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Title, c.Description, c.Column, c.Project, c.Position, c.PaneTarget, c.Labels, c.CardType, c.Priority,
	)
	if err != nil {
		return err
	}
	c.ID, _ = result.LastInsertId()
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	return nil
}

func (d *DB) UpdateCard(c *Card) error {
	_, err := d.db.Exec(
		`UPDATE cards SET title=?, description=?, "column"=?, project=?, position=?, pane_target=?, labels=?, card_type=?, priority=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.Title, c.Description, c.Column, c.Project, c.Position, c.PaneTarget, c.Labels, c.CardType, c.Priority, c.ID,
	)
	return err
}

func (d *DB) MoveCard(id int64, toColumn string, toPosition int) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get the card's project so we scope the shift correctly
	var project string
	if err := tx.QueryRow(`SELECT project FROM cards WHERE id = ?`, id).Scan(&project); err != nil {
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

	return tx.Commit()
}

func (d *DB) DeleteCard(id int64) error {
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
