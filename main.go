package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	workshopmcp "github.com/jamesnhan/workshop/internal/mcp"
	"github.com/jamesnhan/workshop/internal/server"
	"github.com/jamesnhan/workshop/internal/telemetry"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

// Version is set at build time via -ldflags "-X main.Version=<sha>".
// Matches the frontend's VITE_BUILD_VERSION so /api/v1/version can tell a
// stale tab that the server has been upgraded and prompt a reload.
var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[0] != "-" {
		switch os.Args[1] {
		case "mcp":
			workshopmcp.Serve()
			return
		case "version":
			fmt.Println("workshop " + Version)
			return
		}
	}

	// Default: run the web server
	addr := flag.String("addr", ":9090", "listen address")
	flag.Parse()

	// Log level from WORKSHOP_LOG_LEVEL (debug|info|warn|error). Default: info.
	// Raise to debug when chasing intermittent bugs — telemetry pipeline
	// already ships logs to Loki so cranking this up gives a rich timeline.
	level := slog.LevelInfo
	switch os.Getenv("WORKSHOP_LOG_LEVEL") {
	case "debug", "DEBUG":
		level = slog.LevelDebug
	case "warn", "WARN":
		level = slog.LevelWarn
	case "error", "ERROR":
		level = slog.LevelError
	}
	localHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	logger := slog.New(localHandler)
	logger.Info("log level", "level", level.String())

	// Telemetry — no-op when WORKSHOP_OTEL_ENABLED != true.
	otelShutdown, err := telemetry.Init(context.Background(), logger)
	if err != nil {
		logger.Warn("telemetry init failed, continuing without telemetry", "err", err)
	}
	// After Init, the OTel log provider is configured (if enabled). Build
	// a tee logger that writes to both stderr AND the OTel pipeline so logs
	// carry trace_id / span_id in the collector while still showing locally.
	logger = telemetry.NewTeeLogger(localHandler)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(ctx); err != nil {
			logger.Warn("telemetry shutdown error", "err", err)
		}
	}()

	srv, err := server.New(logger, frontendFS, Version)
	if err != nil {
		logger.Error("failed to initialize server", "err", err)
		os.Exit(1)
	}

	logger.Info("starting workshop", "addr", *addr)
	if err := srv.ListenAndServe(*addr); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}
