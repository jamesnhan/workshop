package main

import (
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"os"

	workshopmcp "github.com/jamesnhan/workshop/internal/mcp"
	"github.com/jamesnhan/workshop/internal/server"
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
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
