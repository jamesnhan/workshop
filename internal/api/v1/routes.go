package v1

import (
	"log/slog"
	"net/http"
	"time"

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
	Broadcast(msgType string, data any)
	MarkSeen(target string)
}

// UIHub broadcasts UI commands to the frontend and tracks responses for
// blocking commands like prompt_user/confirm.
type UIHub interface {
	Send(action string, payload map[string]any)
	SendAndWait(action string, payload map[string]any, timeout time.Duration) (UIResponse, error)
	Resolve(id string, resp UIResponse) bool
}

// ChannelHubAPI is the subset of the channel hub the REST layer needs.
type ChannelHubAPI interface {
	Publish(channel, from, body, project string) (*db.ChannelMessageRecord, []string, error)
	Subscribe(channel, target, project string) error
	Unsubscribe(channel, target string) error
	ListChannels(project string) ([]db.Channel, error)
	ListMessages(channel string, limit int) ([]db.ChannelMessageRecord, error)
	RegisterListener(target string) (<-chan ChannelDeliveryMessage, func())
	HasListener(target string) bool
	SetMode(mode string)
	Mode() string
}

// ChannelDeliveryMessage is the payload pushed through native listener channels.
type ChannelDeliveryMessage struct {
	ID        int64     `json:"id"`
	Channel   string    `json:"channel"`
	From      string    `json:"from"`
	Body      string    `json:"body"`
	Project   string    `json:"project,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// UIResponse mirrors server.UIResponse for the API package without an import cycle.
type UIResponse struct {
	Value     any  `json:"value,omitempty"`
	Cancelled bool `json:"cancelled,omitempty"`
}

type API struct {
	logger    *slog.Logger
	tmux      tmux.Bridge
	searcher  OutputSearcher
	db        *db.DB
	consensus *consensus.Engine
	recorder  Recorder
	status    StatusManager
	ui        UIHub
	channels  ChannelHubAPI
}

func New(logger *slog.Logger, bridge tmux.Bridge, searcher OutputSearcher, database *db.DB, consensusEngine *consensus.Engine, recorder Recorder, status StatusManager, ui UIHub, channels ChannelHubAPI) *API {
	return &API{logger: logger, tmux: bridge, searcher: searcher, db: database, consensus: consensusEngine, recorder: recorder, status: status, ui: ui, channels: channels}
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
	mux.HandleFunc("POST /agents/launch", a.handleLaunchAgent)
	mux.HandleFunc("GET /search", a.handleSearch)
	mux.HandleFunc("GET /search/lines", a.handleListLines)
	mux.HandleFunc("GET /search/context", a.handleSearchContext)

	// Kanban
	mux.HandleFunc("GET /cards", a.handleListCards)
	mux.HandleFunc("POST /cards", a.handleCreateCard)
	mux.HandleFunc("GET /cards/{id}", a.handleGetCard)
	mux.HandleFunc("PUT /cards/{id}", a.handleUpdateCard)
	mux.HandleFunc("POST /cards/{id}/move", a.handleMoveCard)
	mux.HandleFunc("DELETE /cards/{id}", a.handleDeleteCard)
	mux.HandleFunc("GET /projects", a.handleListProjects)
	mux.HandleFunc("GET /cards/{id}/notes", a.handleListNotes)
	mux.HandleFunc("POST /cards/{id}/notes", a.handleAddNote)
	mux.HandleFunc("GET /cards/{id}/log", a.handleListCardLog)
	mux.HandleFunc("GET /cards/log", a.handleListProjectLog)
	mux.HandleFunc("GET /cards/{id}/dispatches", a.handleListDispatches)
	mux.HandleFunc("GET /dispatches/active", a.handleListActiveDispatches)

	// Recordings
	mux.HandleFunc("GET /recordings", a.handleListRecordings)
	mux.HandleFunc("POST /recordings", a.handleStartRecording)
	mux.HandleFunc("GET /recordings/{id}", a.handleGetRecording)
	mux.HandleFunc("POST /recordings/{id}/stop", a.handleStopRecording)
	mux.HandleFunc("DELETE /recordings/{id}", a.handleDeleteRecording)

	// Pane status
	mux.HandleFunc("POST /panes/status", a.handleSetPaneStatus)
	mux.HandleFunc("DELETE /panes/status", a.handleClearPaneStatus)

	// Channels (inter-pane agent messaging)
	mux.HandleFunc("POST /channels/publish", a.handleChannelPublish)
	mux.HandleFunc("POST /channels/subscribe", a.handleChannelSubscribe)
	mux.HandleFunc("POST /channels/unsubscribe", a.handleChannelUnsubscribe)
	mux.HandleFunc("GET /channels", a.handleChannelList)
	mux.HandleFunc("GET /channels/{name}/messages", a.handleChannelMessages)
	mux.HandleFunc("GET /channel-listen/{target}", a.handleChannelListen)
	mux.HandleFunc("GET /channel-mode", a.handleChannelMode)
	mux.HandleFunc("PUT /channel-mode", a.handleChannelMode)

	// UI control (frontend manipulation from agents)
	mux.HandleFunc("POST /ui/assign_pane", a.handleUIAction("assign_pane", false))
	mux.HandleFunc("POST /ui/focus_cell", a.handleUIAction("focus_cell", false))
	mux.HandleFunc("POST /ui/focus_pane", a.handleUIAction("focus_pane", false))
	mux.HandleFunc("POST /ui/switch_view", a.handleUIAction("switch_view", false))
	mux.HandleFunc("POST /ui/show_toast", a.handleUIAction("show_toast", false))
	mux.HandleFunc("POST /ui/open_card", a.handleUIAction("open_card", false))
	mux.HandleFunc("POST /ui/prompt_user", a.handleUIAction("prompt_user", true))
	mux.HandleFunc("POST /ui/confirm", a.handleUIAction("confirm", true))
	mux.HandleFunc("POST /ui/response/{id}", a.handleUIResponse)

	// Git
	mux.HandleFunc("GET /git/info", a.handleGitInfo)

	// Docs
	mux.HandleFunc("GET /docs/read", a.handleReadFile)
	mux.HandleFunc("GET /docs/list", a.handleListMarkdown)
	mux.HandleFunc("POST /docs/open", a.handleOpenDoc)

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
