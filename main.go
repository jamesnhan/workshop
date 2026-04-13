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

func main() {
	if len(os.Args) > 1 && os.Args[0] != "-" {
		switch os.Args[1] {
		case "mcp":
			workshopmcp.Serve()
			return
		case "version":
			fmt.Println("workshop v0.1.0")
			return
		}
	}

	// Default: run the web server
	addr := flag.String("addr", ":9090", "listen address")
	flag.Parse()

	localHandler := slog.NewTextHandler(os.Stderr, nil)
	logger := slog.New(localHandler)

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

	srv, err := server.New(logger, frontendFS)
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
