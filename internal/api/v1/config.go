package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesnhan/workshop/internal/config"
	"github.com/jamesnhan/workshop/internal/ollama"
)

// isAllowedConfigPath restricts config loading to ~/.config/workshop/.
func isAllowedConfigPath(path string) bool {
	home := os.Getenv("HOME")
	if home == "" {
		return false
	}
	allowed := filepath.Join(home, ".config", "workshop")
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve symlinks to prevent escaping via symlink
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File might not exist yet — check the dir instead
		real = abs
	}
	return strings.HasPrefix(real, allowed)
}

func (a *API) handleLoadConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		a.jsonError(w, "path is required", http.StatusBadRequest)
		return
	}

	if !isAllowedConfigPath(req.Path) {
		a.jsonError(w, "config path not allowed — must be under ~/.config/workshop/", http.StatusForbidden)
		return
	}

	engine := config.NewLuaEngine(a.tmux, a.logger)
	defer engine.Close()

	err := engine.RunFile(req.Path)
	if err != nil {
		a.jsonError(w, "config error", http.StatusBadRequest)
		return
	}

	// Wire up Ollama endpoints if configured
	if len(engine.Result.OllamaEndpoints) > 0 {
		eps := make([]ollama.Endpoint, len(engine.Result.OllamaEndpoints))
		for i, e := range engine.Result.OllamaEndpoints {
			eps[i] = ollama.Endpoint{Name: e.Name, URL: e.URL, Default: e.Default}
		}
		a.ollama = ollama.NewClient(eps)
		a.logger.Info("ollama endpoints configured", "count", len(eps))
	}

	a.jsonOK(w, engine.Result)
}

func (a *API) handleFindConfig(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}
	path := config.FindConfig(dir)
	a.jsonOK(w, map[string]string{"path": path})
}
