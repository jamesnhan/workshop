package v1

import (
	"encoding/json"
	"net/http"

	"github.com/jamesnhan/workshop/internal/config"
)

func (a *API) handleLoadConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Code string `json:"code"` // inline Lua code (alternative to path)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	engine := config.NewLuaEngine(a.tmux, a.logger)
	defer engine.Close()

	var err error
	if req.Path != "" {
		err = engine.RunFile(req.Path)
	} else if req.Code != "" {
		err = engine.RunString(req.Code)
	} else {
		a.jsonError(w, "path or code is required", http.StatusBadRequest)
		return
	}

	if err != nil {
		a.jsonError(w, "config error", http.StatusBadRequest)
		return
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
