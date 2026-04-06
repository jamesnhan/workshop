package v1

import (
	"log/slog"
	"net/http"

	"github.com/jamesnhan/workshop/internal/consensus"
	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/tmux"
)

// OutputSearcher is implemented by the output buffer.
type OutputSearcher interface {
	SearchJSON(query, target string, limit int) []map[string]any
	ListAll(target string) []map[string]any
	GetContext(target string, line, contextLines int) []string
}

// Recorder manages pane recordings independently of WebSocket connections.
type Recorder interface {
	Start(target, name string, cols, rows int) (int64, error)
	Stop(target string) (int64, error)
	IsRecording(target string) int64
}

// StatusManager manages pane status indicators.
type StatusManager interface {
	Set(target, status, message string)
	Clear(target string)
}

type API struct {
	logger    *slog.Logger
	tmux      tmux.Bridge
	searcher  OutputSearcher
	db        *db.DB
	consensus *consensus.Engine
	recorder  Recorder
	status    StatusManager
}

func New(logger *slog.Logger, bridge tmux.Bridge, searcher OutputSearcher, database *db.DB, consensusEngine *consensus.Engine, recorder Recorder, status StatusManager) *API {
	return &API{logger: logger, tmux: bridge, searcher: searcher, db: database, consensus: consensusEngine, recorder: recorder, status: status}
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /sessions", a.handleListSessions)
	mux.HandleFunc("POST /sessions", a.handleCreateSession)
	mux.HandleFunc("DELETE /sessions/{name}", a.handleKillSession)
	mux.HandleFunc("PATCH /sessions/{name}", a.handleRenameSession)
	mux.HandleFunc("POST /sessions/{name}/send-keys", a.handleSendKeys)
	mux.HandleFunc("GET /sessions/{name}/capture", a.handleCapturePane)
	mux.HandleFunc("GET /sessions/{name}/panes", a.handleListPanes)
	mux.HandleFunc("POST /sessions/{name}/windows", a.handleCreateWindow)
	mux.HandleFunc("PATCH /windows/{target}", a.handleRenameWindow)
	mux.HandleFunc("GET /panes", a.handleListAllPanes)
	mux.HandleFunc("GET /agents/providers", a.handleListProviders)
	mux.HandleFunc("POST /agents/launch", a.handleLaunchAgent)
	mux.HandleFunc("GET /search", a.handleSearch)
	mux.HandleFunc("GET /search/lines", a.handleListLines)
	mux.HandleFunc("GET /search/context", a.handleSearchContext)

	// Kanban
	mux.HandleFunc("GET /cards", a.handleListCards)
	mux.HandleFunc("POST /cards", a.handleCreateCard)
	mux.HandleFunc("PUT /cards/{id}", a.handleUpdateCard)
	mux.HandleFunc("POST /cards/{id}/move", a.handleMoveCard)
	mux.HandleFunc("DELETE /cards/{id}", a.handleDeleteCard)
	mux.HandleFunc("GET /projects", a.handleListProjects)
	mux.HandleFunc("GET /cards/{id}/notes", a.handleListNotes)
	mux.HandleFunc("POST /cards/{id}/notes", a.handleAddNote)

	// Recordings
	mux.HandleFunc("GET /recordings", a.handleListRecordings)
	mux.HandleFunc("POST /recordings", a.handleStartRecording)
	mux.HandleFunc("GET /recordings/{id}", a.handleGetRecording)
	mux.HandleFunc("POST /recordings/{id}/stop", a.handleStopRecording)
	mux.HandleFunc("DELETE /recordings/{id}", a.handleDeleteRecording)

	// Pane status
	mux.HandleFunc("POST /panes/status", a.handleSetPaneStatus)
	mux.HandleFunc("DELETE /panes/status", a.handleClearPaneStatus)

	// Git
	mux.HandleFunc("GET /git/info", a.handleGitInfo)

	// Docs
	mux.HandleFunc("GET /docs/read", a.handleReadFile)
	mux.HandleFunc("GET /docs/list", a.handleListMarkdown)

	// Config
	mux.HandleFunc("POST /config/load", a.handleLoadConfig)
	mux.HandleFunc("GET /config/find", a.handleFindConfig)

	// Consensus
	mux.HandleFunc("POST /consensus", a.handleStartConsensus)
	mux.HandleFunc("GET /consensus", a.handleListConsensus)
	mux.HandleFunc("GET /consensus/{id}", a.handleGetConsensus)
	mux.HandleFunc("DELETE /consensus/{id}", a.handleCleanupConsensus)

	return mux
}
