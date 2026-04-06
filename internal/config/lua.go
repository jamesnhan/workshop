package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jamesnhan/workshop/internal/tmux"
	lua "github.com/yuin/gopher-lua"
)

// LuaEngine runs Lua config scripts with access to Workshop's API.
type LuaEngine struct {
	L      *lua.LState
	bridge tmux.Bridge
	logger *slog.Logger
	// Results collected from Lua execution
	Result *ConfigResult
}

// ConfigResult holds the config values set by Lua scripts.
type ConfigResult struct {
	Theme      string            `json:"theme,omitempty"`
	GridRows   int               `json:"gridRows,omitempty"`
	GridCols   int               `json:"gridCols,omitempty"`
	Sessions   []SessionConfig   `json:"sessions,omitempty"`
	Agents     []AgentPreset     `json:"agents,omitempty"`
	OnStartup  []string          `json:"onStartup,omitempty"` // commands to run on startup
}

type SessionConfig struct {
	Name      string `json:"name"`
	Directory string `json:"directory,omitempty"`
	Command   string `json:"command,omitempty"` // command to run after creating
}

type AgentPreset struct {
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"` // claude, gemini, codex
	Model     string `json:"model,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Directory string `json:"directory,omitempty"`
}

func NewLuaEngine(bridge tmux.Bridge, logger *slog.Logger) *LuaEngine {
	L := lua.NewState()
	engine := &LuaEngine{
		L:      L,
		bridge: bridge,
		logger: logger,
		Result: &ConfigResult{},
	}
	engine.registerAPI()
	return engine
}

func (e *LuaEngine) Close() {
	e.L.Close()
}

// RunFile executes a Lua config file.
func (e *LuaEngine) RunFile(path string) error {
	return e.L.DoFile(path)
}

// RunString executes a Lua config string.
func (e *LuaEngine) RunString(code string) error {
	return e.L.DoString(code)
}

// FindConfig looks for workshop.lua in the given directory, then parent dirs.
func FindConfig(dir string) string {
	for {
		path := filepath.Join(dir, "workshop.lua")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// registerAPI exposes Workshop functions to Lua.
func (e *LuaEngine) registerAPI() {
	// Create the "workshop" module table
	mod := e.L.NewTable()

	// workshop.theme(name)
	mod.RawSetString("theme", e.L.NewFunction(func(L *lua.LState) int {
		e.Result.Theme = L.CheckString(1)
		return 0
	}))

	// workshop.grid(rows, cols)
	mod.RawSetString("grid", e.L.NewFunction(func(L *lua.LState) int {
		e.Result.GridRows = L.CheckInt(1)
		e.Result.GridCols = L.CheckInt(2)
		return 0
	}))

	// workshop.session(name, [dir], [command])
	mod.RawSetString("session", e.L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		dir := L.OptString(2, "")
		cmd := L.OptString(3, "")
		e.Result.Sessions = append(e.Result.Sessions, SessionConfig{
			Name:      name,
			Directory: dir,
			Command:   cmd,
		})
		return 0
	}))

	// workshop.agent(config_table)
	// workshop.agent({ name = "reviewer", model = "sonnet", prompt = "..." })
	mod.RawSetString("agent", e.L.NewFunction(func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		preset := AgentPreset{}
		tbl.ForEach(func(key, val lua.LValue) {
			switch key.String() {
			case "name":
				preset.Name = val.String()
			case "provider":
				preset.Provider = val.String()
			case "model":
				preset.Model = val.String()
			case "prompt":
				preset.Prompt = val.String()
			case "directory":
				preset.Directory = val.String()
			}
		})
		if preset.Name == "" {
			preset.Name = fmt.Sprintf("agent-%d", len(e.Result.Agents)+1)
		}
		e.Result.Agents = append(e.Result.Agents, preset)
		return 0
	}))

	// workshop.run(command) — queue a shell command to run on startup
	mod.RawSetString("run", e.L.NewFunction(func(L *lua.LState) int {
		cmd := L.CheckString(1)
		e.Result.OnStartup = append(e.Result.OnStartup, cmd)
		return 0
	}))

	// workshop.create_session(name, [dir]) — immediately create a tmux session
	mod.RawSetString("create_session", e.L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		dir := L.OptString(2, "")
		if err := e.bridge.CreateSession(name, dir); err != nil {
			L.ArgError(1, err.Error())
			return 0
		}
		e.logger.Info("lua: created session", "name", name)
		return 0
	}))

	// workshop.send_keys(target, command) — send keys to a pane
	mod.RawSetString("send_keys", e.L.NewFunction(func(L *lua.LState) int {
		target := L.CheckString(1)
		keys := L.CheckString(2)
		if err := e.bridge.SendKeys(target, keys); err != nil {
			L.ArgError(1, err.Error())
		}
		return 0
	}))

	// workshop.list_sessions() — returns a table of session names
	mod.RawSetString("list_sessions", e.L.NewFunction(func(L *lua.LState) int {
		sessions, err := e.bridge.ListSessions()
		if err != nil {
			L.Push(lua.LNil)
			return 1
		}
		tbl := L.NewTable()
		for _, s := range sessions {
			tbl.Append(lua.LString(s.Name))
		}
		L.Push(tbl)
		return 1
	}))

	// workshop.env(name) — get environment variable
	mod.RawSetString("env", e.L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		L.Push(lua.LString(os.Getenv(name)))
		return 1
	}))

	// workshop.log(message)
	mod.RawSetString("log", e.L.NewFunction(func(L *lua.LState) int {
		msg := L.CheckString(1)
		e.logger.Info("lua: " + msg)
		return 0
	}))

	e.L.SetGlobal("workshop", mod)
}
