package v1

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/ollama"
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

// ApprovalHubAPI is the subset of the approval hub the REST layer needs.
type ApprovalHubAPI interface {
	WaitForDecision(approvalID int64, payload map[string]any, timeout time.Duration) string
	Resolve(approvalID int64, decision string) bool
}

type API struct {
	logger    *slog.Logger
	tmux      tmux.Bridge
	searcher  OutputSearcher
	db        *db.DB
	recorder  Recorder
	status    StatusManager
	ui        UIHub
	channels  ChannelHubAPI
	ollama    *ollama.Client
	approvals ApprovalHubAPI
	tmuxProxy *tmuxProxy
	uploadDir string
}

func New(logger *slog.Logger, bridge tmux.Bridge, searcher OutputSearcher, database *db.DB, recorder Recorder, status StatusManager, ui UIHub, channels ChannelHubAPI) *API {
	return &API{logger: logger, tmux: bridge, searcher: searcher, db: database, recorder: recorder, status: status, ui: ui, channels: channels}
}

// SetUploadDir sets the directory for file uploads. Falls back to /tmp/workshop-uploads.
func (a *API) SetUploadDir(dir string) {
	a.uploadDir = dir
}

// SetOllama configures the Ollama client for local model integration.
func (a *API) SetOllama(c *ollama.Client) {
	a.ollama = c
}

// SetApprovalHub configures the approval hub for blocking approval requests.
func (a *API) SetApprovalHub(hub ApprovalHubAPI) {
	a.approvals = hub
}

func (a *API) Routes() http.Handler {
	mux := http.NewServeMux()

	// tmuxHandler returns the proxy handler for tmux-dependent routes when a
	// proxy is configured, otherwise the normal handler (which will 503 via
	// requireTmux if headless).
	tmuxHandler := func(normal http.HandlerFunc) http.HandlerFunc {
		if a.tmuxProxy != nil {
			return a.tmuxProxy.ServeHTTP
		}
		return normal
	}

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /init", a.handleInit)
	mux.HandleFunc("POST /debug/log", a.handleDebugLog)
	mux.HandleFunc("POST /upload", a.handleUpload)
	mux.HandleFunc("GET /sessions", tmuxHandler(a.handleListSessions))
	mux.HandleFunc("POST /sessions", tmuxHandler(a.handleCreateSession))
	mux.HandleFunc("DELETE /sessions/{name}", tmuxHandler(a.handleKillSession))
	mux.HandleFunc("PATCH /sessions/{name}", tmuxHandler(a.handleRenameSession))
	mux.HandleFunc("POST /sessions/{name}/send-keys", tmuxHandler(a.handleSendKeys))
	mux.HandleFunc("GET /sessions/{name}/capture", tmuxHandler(a.handleCapturePane))
	mux.HandleFunc("GET /sessions/{name}/panes", tmuxHandler(a.handleListPanes))
	mux.HandleFunc("POST /sessions/{name}/windows", tmuxHandler(a.handleCreateWindow))
	mux.HandleFunc("PATCH /windows/{target}", tmuxHandler(a.handleRenameWindow))
	mux.HandleFunc("GET /panes", tmuxHandler(a.handleListAllPanes))
	mux.HandleFunc("POST /agents/launch", tmuxHandler(a.handleLaunchAgent))
	mux.HandleFunc("GET /search", tmuxHandler(a.handleSearch))
	mux.HandleFunc("GET /search/lines", tmuxHandler(a.handleListLines))
	mux.HandleFunc("GET /search/context", tmuxHandler(a.handleSearchContext))

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
	mux.HandleFunc("GET /cards/{id}/messages", a.handleListMessages)
	mux.HandleFunc("POST /cards/{id}/messages", a.handleAddMessage)
	mux.HandleFunc("GET /cards/{id}/log", a.handleListCardLog)
	mux.HandleFunc("GET /cards/log", a.handleListProjectLog)
	mux.HandleFunc("GET /card-dependencies", a.handleListDependencies)
	mux.HandleFunc("POST /cards/{id}/blocks", a.handleAddDependency)
	mux.HandleFunc("DELETE /cards/{id}/blocks/{blockerId}", a.handleRemoveDependency)

	// Workflows
	mux.HandleFunc("GET /workflows", a.handleGetWorkflow)
	mux.HandleFunc("PUT /workflows", a.handleSetWorkflow)

	// Activity log
	mux.HandleFunc("POST /activity", a.handleRecordActivity)
	mux.HandleFunc("GET /activity", a.handleListActivity)

	// Approvals
	mux.HandleFunc("POST /approvals", a.handleRequestApproval)
	mux.HandleFunc("GET /approvals", a.handleListApprovals)
	mux.HandleFunc("POST /approvals/{id}/resolve", a.handleResolveApproval)

	// Agent presets
	mux.HandleFunc("GET /agent-presets", a.handleListPresets)
	mux.HandleFunc("PUT /agent-presets", a.handleUpsertPreset)
	mux.HandleFunc("DELETE /agent-presets/{name}", a.handleDeletePreset)

	// Claude Code session analysis
	mux.HandleFunc("GET /compactions", a.handleListCompactions)
	mux.HandleFunc("GET /session-usage", a.handleSessionUsage)

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
	mux.HandleFunc("GET /git/commit", a.handleGitCommit)

	// Docs
	mux.HandleFunc("GET /docs/read", a.handleReadFile)
	mux.HandleFunc("GET /docs/list", a.handleListMarkdown)
	mux.HandleFunc("GET /docs/search", a.handleSearchDocs)
	mux.HandleFunc("POST /docs/open", a.handleOpenDoc)

	// Ollama (local LLM)
	mux.HandleFunc("GET /ollama/models", a.handleOllamaModels)
	mux.HandleFunc("GET /ollama/health", a.handleOllamaHealth)
	mux.HandleFunc("POST /ollama/chat", a.handleOllamaChat)
	mux.HandleFunc("POST /ollama/generate", a.handleOllamaGenerate)
	mux.HandleFunc("GET /ollama/conversations", a.handleListConversations)
	mux.HandleFunc("POST /ollama/conversations", a.handleCreateConversation)
	mux.HandleFunc("GET /ollama/conversations/{id}", a.handleGetConversation)
	mux.HandleFunc("PUT /ollama/conversations/{id}", a.handleUpdateConversation)
	mux.HandleFunc("DELETE /ollama/conversations/{id}", a.handleDeleteConversation)
	mux.HandleFunc("POST /ollama/conversations/{id}/messages", a.handleCreateConversationMessage)

	// Usage tracking
	mux.HandleFunc("POST /usage", a.handleRecordUsage)
	mux.HandleFunc("GET /usage", a.handleListUsage)
	mux.HandleFunc("GET /usage/aggregate", a.handleAggregateUsage)
	mux.HandleFunc("GET /usage/daily-spend", a.handleDailySpend)

	// Link preview
	mux.HandleFunc("GET /link-preview", a.handleLinkPreview)

	// Config
	mux.HandleFunc("POST /config/load", a.handleLoadConfig)
	mux.HandleFunc("GET /config/find", a.handleFindConfig)

	return mux
}
