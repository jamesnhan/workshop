package server

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	apiv1 "github.com/jamesnhan/workshop/internal/api/v1"
	"github.com/jamesnhan/workshop/internal/config"
	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/ollama"
	"github.com/jamesnhan/workshop/internal/telemetry"
	"github.com/jamesnhan/workshop/internal/tmux"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
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
	handler    http.Handler
	logger     *slog.Logger
	db         *db.DB
	recorder   *RecordingManager
	stopReaper chan struct{}
}

func New(logger *slog.Logger, frontendFS embed.FS, version string) (*Server, error) {
	headless := os.Getenv("WORKSHOP_HEADLESS") == "true"

	if !headless {
		tmux.CleanupStaleControlSessions()
	}

	dataDir := os.Getenv("WORKSHOP_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "workshop")
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	logger.Info("database opened", "path", filepath.Join(dataDir, "workshop.db"), "headless", headless)

	mux := http.NewServeMux()

	var bridge tmux.Bridge
	if headless {
		bridge = tmux.NewNoBridge()
	} else {
		bridge = tmux.NewExecBridge("")
	}
	outputBuffer := NewOutputBuffer(10000)
	recorder := NewRecordingManager(logger, database)
	statusStore := NewStatusStore()
	paneMonitor := NewPaneMonitor(bridge, statusStore, logger)
	statusStore.AttachMonitor(paneMonitor)
	if !headless {
		paneMonitor.Start(make(chan struct{})) // runs for the lifetime of the server
	}
	uiHub := NewUICommandHub(statusStore)

	// In headless mode, force native-only channel delivery (no compat/send_text)
	channelMode := DeliveryAuto
	if headless {
		channelMode = DeliveryNative
	}
	channelHub := NewChannelHub(database, logger, NewSendTextDelivery(bridge), channelMode)
	approvalHub := NewApprovalHub(statusStore)
	api := apiv1.New(logger, bridge, outputBuffer, database, recorder, statusStore, &uiHubAdapter{hub: uiHub}, &channelHubAdapter{hub: channelHub})
	api.SetApprovalHub(approvalHub)
	api.SetVersion(version)

	// Upload directory: persist uploads alongside the DB so they survive
	// pod restarts on K8s (both use WORKSHOP_DATA_DIR / PVC).
	uploadDir := filepath.Join(dataDir, "uploads")
	api.SetUploadDir(uploadDir)
	stopReaper := make(chan struct{})
	apiv1.StartUploadReaper(uploadDir, 30*24*time.Hour, logger, stopReaper)

	// Configure tmux reverse proxy for headless mode.
	if proxyURL := os.Getenv("WORKSHOP_TMUX_PROXY_URL"); proxyURL != "" && headless {
		if err := api.SetTmuxProxy(proxyURL); err != nil {
			logger.Warn("failed to set tmux proxy", "url", proxyURL, "err", err)
		} else {
			logger.Info("tmux proxy enabled", "target", proxyURL)
		}
	}

	// Auto-load Ollama endpoints: env var first, then Lua config fallback.
	// WORKSHOP_OLLAMA_ENDPOINTS format: "name=url,name=url" (first is default)
	if envEps := os.Getenv("WORKSHOP_OLLAMA_ENDPOINTS"); envEps != "" {
		var eps []ollama.Endpoint
		for i, entry := range strings.Split(envEps, ",") {
			parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(parts) == 2 {
				eps = append(eps, ollama.Endpoint{Name: parts[0], URL: parts[1], Default: i == 0})
			} else if len(parts) == 1 && parts[0] != "" {
				eps = append(eps, ollama.Endpoint{Name: "default", URL: parts[0], Default: i == 0})
			}
		}
		if len(eps) > 0 {
			api.SetOllama(ollama.NewClient(eps))
			logger.Info("ollama endpoints loaded from env", "count", len(eps))
		}
	} else if cfgPath := config.FindConfig(filepath.Join(os.Getenv("HOME"), ".config", "workshop")); cfgPath != "" {
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

	apiHandler := http.Handler(api.Routes())
	if telemetry.Enabled() {
		// Wrap API routes with otelhttp INSIDE StripPrefix so that
		// Request.Pattern from the inner mux (e.g. "GET /cards/{id}")
		// is visible. We prepend /api/v1 to restore the full path.
		apiHandler = otelhttp.NewHandler(apiHandler, "workshop-api",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if pat := r.Pattern; pat != "" {
					return r.Method + " /api/v1" + pat[strings.Index(pat, " ")+1:]
				}
				return r.Method + " /api/v1" + r.URL.Path
			}),
			otelhttp.WithMetricAttributesFn(func(r *http.Request) []attribute.KeyValue {
				route := r.URL.Path
				if pat := r.Pattern; pat != "" {
					// Pattern is "GET /cards/{id}" — extract just the path part
					if idx := strings.Index(pat, " "); idx >= 0 {
						route = pat[idx+1:]
					}
				}
				return []attribute.KeyValue{
					attribute.String("http.route", "/api/v1"+route),
				}
			}),
		)
	}
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", apiHandler))
	// Proxy the WebSocket to the desktop in headless mode, otherwise handle locally.
	if proxyURL := api.TmuxProxyURL(); proxyURL != "" {
		mux.Handle("GET /ws", newWSProxy(proxyURL))
		logger.Info("websocket proxied to desktop", "target", proxyURL)
	} else {
		mux.HandleFunc("GET /ws", wsHandler(logger, bridge, outputBuffer, recorder, statusStore))
	}
	mux.Handle("/", spaHandler(frontendFS))

	// Wrap with API key auth if WORKSHOP_API_KEY is set.
	apiKey := os.Getenv("WORKSHOP_API_KEY")
	if apiKey != "" {
		logger.Info("API key authentication enabled")
	}
	handler := authMiddleware(apiKey, mux)

	return &Server{handler: handler, logger: logger, db: database, recorder: recorder, stopReaper: stopReaper}, nil
}

func (s *Server) ListenAndServe(addr string) error {
	defer s.db.Close()
	defer s.recorder.StopAll()
	defer close(s.stopReaper)

	handler := http.Handler(s.handler)
	// Outer otelhttp wrap for non-API routes only (WS, static files).
	// API routes have their own otelhttp wrap inside StripPrefix with
	// specific route patterns, so we skip them here to avoid double-counting.
	if telemetry.Enabled() {
		handler = otelhttp.NewHandler(handler, "workshop-http",
			otelhttp.WithFilter(func(r *http.Request) bool {
				return !strings.HasPrefix(r.URL.Path, "/api/v1/")
			}),
		)
	}
	return http.ListenAndServe(addr, handler)
}
