package server

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	apiv1 "github.com/jamesnhan/workshop/internal/api/v1"
	"github.com/jamesnhan/workshop/internal/consensus"
	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/tmux"
)

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
	consensusEngine := consensus.NewEngine(bridge, logger)
	api := apiv1.New(logger, bridge, outputBuffer, database, consensusEngine, recorder, statusStore)

	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", api.Routes()))
	mux.HandleFunc("GET /ws", wsHandler(logger, bridge, outputBuffer, recorder, statusStore))
	mux.Handle("/", spaHandler(frontendFS))

	return &Server{handler: mux, logger: logger, db: database, recorder: recorder}, nil
}

func (s *Server) ListenAndServe(addr string) error {
	defer s.db.Close()
	defer s.recorder.StopAll()
	return http.ListenAndServe(addr, s.handler)
}
