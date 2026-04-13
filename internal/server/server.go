package server

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	apiv1 "github.com/jamesnhan/workshop/internal/api/v1"
	"github.com/jamesnhan/workshop/internal/config"
	"github.com/jamesnhan/workshop/internal/consensus"
	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/ollama"
	"github.com/jamesnhan/workshop/internal/telemetry"
	"github.com/jamesnhan/workshop/internal/tmux"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// channelHubAdapter bridges server.ChannelHub to apiv1.ChannelHubAPI,
// converting between the two packages' message types.
type channelHubAdapter struct {
	hub *ChannelHub
}

func (a *channelHubAdapter) Publish(channel, from, body, project string) (*db.ChannelMessageRecord, []string, error) {
	return a.hub.Publish(channel, from, body, project)
}

func (a *channelHubAdapter) Subscribe(channel, target, project string) error {
	return a.hub.Subscribe(channel, target, project)
}

func (a *channelHubAdapter) Unsubscribe(channel, target string) error {
	return a.hub.Unsubscribe(channel, target)
}

func (a *channelHubAdapter) ListChannels(project string) ([]db.Channel, error) {
	return a.hub.ListChannels(project)
}

func (a *channelHubAdapter) ListMessages(channel string, limit int) ([]db.ChannelMessageRecord, error) {
	return a.hub.ListMessages(channel, limit)
}

func (a *channelHubAdapter) RegisterListener(target string) (<-chan apiv1.ChannelDeliveryMessage, func()) {
	src, cancel := a.hub.RegisterListener(target)
	out := make(chan apiv1.ChannelDeliveryMessage, 16)
	go func() {
		defer close(out)
		for msg := range src {
			out <- apiv1.ChannelDeliveryMessage{
				ID:        msg.ID,
				Channel:   msg.Channel,
				From:      msg.From,
				Body:      msg.Body,
				Project:   msg.Project,
				Timestamp: msg.Timestamp,
			}
		}
	}()
	return out, cancel
}

func (a *channelHubAdapter) HasListener(target string) bool {
	return a.hub.HasListener(target)
}

func (a *channelHubAdapter) SetMode(mode string) {
	a.hub.SetMode(DeliveryMode(mode))
}

func (a *channelHubAdapter) Mode() string {
	return string(a.hub.Mode())
}

// uiHubAdapter bridges server.UICommandHub to apiv1.UIHub, converting
// between the two packages' UIResponse types.
type uiHubAdapter struct {
	hub *UICommandHub
}

func (a *uiHubAdapter) Send(action string, payload map[string]any) {
	a.hub.Send(action, payload)
}

func (a *uiHubAdapter) SendAndWait(action string, payload map[string]any, timeout time.Duration) (apiv1.UIResponse, error) {
	r, err := a.hub.SendAndWait(action, payload, timeout)
	return apiv1.UIResponse{Value: r.Value, Cancelled: r.Cancelled}, err
}

func (a *uiHubAdapter) Resolve(id string, resp apiv1.UIResponse) bool {
	return a.hub.Resolve(id, UIResponse{Value: resp.Value, Cancelled: resp.Cancelled})
}

type Server struct {
	handler  http.Handler
	logger   *slog.Logger
	db       *db.DB
	recorder *RecordingManager
}

func New(logger *slog.Logger, frontendFS embed.FS) (*Server, error) {
	tmux.CleanupStaleControlSessions()

	dataDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "workshop")
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	logger.Info("database opened", "path", filepath.Join(dataDir, "workshop.db"))

	mux := http.NewServeMux()

	bridge := tmux.NewExecBridge("")
	outputBuffer := NewOutputBuffer(10000)
	recorder := NewRecordingManager(logger, database)
	statusStore := NewStatusStore()
	paneMonitor := NewPaneMonitor(bridge, statusStore, logger)
	statusStore.AttachMonitor(paneMonitor)
	paneMonitor.Start(make(chan struct{})) // runs for the lifetime of the server
	uiHub := NewUICommandHub(statusStore)
	channelHub := NewChannelHub(database, logger, NewSendTextDelivery(bridge), DeliveryAuto)
	consensusEngine := consensus.NewEngine(bridge, database, logger)
	approvalHub := NewApprovalHub(statusStore)
	api := apiv1.New(logger, bridge, outputBuffer, database, consensusEngine, recorder, statusStore, &uiHubAdapter{hub: uiHub}, &channelHubAdapter{hub: channelHub})
	api.SetApprovalHub(approvalHub)

	// Auto-load Ollama endpoints from config at startup
	if cfgPath := config.FindConfig(filepath.Join(os.Getenv("HOME"), ".config", "workshop")); cfgPath != "" {
		engine := config.NewLuaEngine(bridge, logger)
		if err := engine.RunFile(cfgPath); err != nil {
			logger.Warn("failed to load startup config for ollama", "path", cfgPath, "err", err)
		} else if len(engine.Result.OllamaEndpoints) > 0 {
			eps := make([]ollama.Endpoint, len(engine.Result.OllamaEndpoints))
			for i, e := range engine.Result.OllamaEndpoints {
				eps[i] = ollama.Endpoint{Name: e.Name, URL: e.URL, Default: e.Default}
			}
			api.SetOllama(ollama.NewClient(eps))
			logger.Info("ollama endpoints loaded from config", "count", len(eps), "path", cfgPath)
		}
		engine.Close()
	}

	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", api.Routes()))
	mux.HandleFunc("GET /ws", wsHandler(logger, bridge, outputBuffer, recorder, statusStore))
	mux.Handle("/", spaHandler(frontendFS))

	return &Server{handler: mux, logger: logger, db: database, recorder: recorder}, nil
}

func (s *Server) ListenAndServe(addr string) error {
	defer s.db.Close()
	defer s.recorder.StopAll()

	handler := http.Handler(s.handler)
	// Wrap with otelhttp if telemetry is enabled — adds a span per HTTP
	// request with method, route, status, and duration attributes.
	if telemetry.Enabled() {
		handler = otelhttp.NewHandler(handler, "workshop-http")
	}
	return http.ListenAndServe(addr, handler)
}
