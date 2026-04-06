package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Session represents a tmux session.
type Session struct {
	Name     string `json:"name"`
	Windows  int    `json:"windows"`
	Created  string `json:"created"`
	Attached bool   `json:"attached"`
	Hidden   bool   `json:"hidden,omitempty"` // true for internal sessions (workshop-ctrl-*, consensus-*)
}

// Pane represents a tmux pane.
type Pane struct {
	ID         string `json:"id"`
	Target     string `json:"target"`     // e.g. "session:window.pane"
	WindowName string `json:"windowName"` // e.g. "claude"
	Command    string `json:"command"`    // e.g. "claude"
	Path       string `json:"path"`       // e.g. "/home/james/repos/workshop"
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Active     bool   `json:"active"`
}

// Bridge defines operations for managing tmux sessions and panes.
type Bridge interface {
	ListSessions() ([]Session, error)
	CreateSession(name, startDir string) error
	KillSession(name string) error
	RenameSession(oldName, newName string) error
	RenameWindow(target, newName string) error
	CreateWindow(session, name string) error
	SplitWindow(target string, horizontal bool) (string, error)
	SendKeys(target, keys string) error
	SendKeysLiteral(target, keys string) error
	SendKeysHex(target, hexStr string) error
	SendInput(target, data string) error
	CapturePane(target string, lines int) (string, error)
	CapturePanePlain(target string, lines int) (string, error)
	CapturePaneAll(target string) (string, error)
ResizePane(target string, cols, rows int) error
	PaneTTY(target string) (string, error)
	ListPanes(session string) ([]Pane, error)
	LaunchAgent(cfg AgentConfig) (*AgentResult, error)
}

// CommandRunner executes a command and returns its combined output.
// Defaults to exec.Command; replaceable for testing.
type CommandRunner func(name string, args ...string) *exec.Cmd

// ExecBridge implements Bridge by shelling out to the tmux binary.
type ExecBridge struct {
	tmuxPath string
	runCmd   CommandRunner
}

// NewExecBridge creates a new ExecBridge. Pass "" for tmuxPath to use "tmux" from PATH.
func NewExecBridge(tmuxPath string) *ExecBridge {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}
	return &ExecBridge{tmuxPath: tmuxPath, runCmd: exec.Command}
}

// RunRaw exposes the raw tmux command execution for special cases.
func (b *ExecBridge) RunRaw(args ...string) (string, error) {
	return b.run(args...)
}

func (b *ExecBridge) run(args ...string) (string, error) {
	cmd := b.runCmd(b.tmuxPath, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (b *ExecBridge) ListSessions() ([]Session, error) {
	out, err := b.run("list-sessions", "-F",
		"#{session_name}\t#{session_windows}\t#{session_created}\t#{session_attached}")
	if err != nil {
		// "no server running" means no sessions, not an error
		if strings.Contains(out, "no server running") || strings.Contains(out, "no current") {
			return nil, nil
		}
		return nil, fmt.Errorf("list-sessions: %w: %s", err, out)
	}
	if out == "" {
		return nil, nil
	}

	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		name := parts[0]
		hidden := strings.HasPrefix(name, "workshop-ctrl-") || strings.HasPrefix(name, "consensus-")
		// Skip internal sessions by default
		if hidden {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		sessions = append(sessions, Session{
			Name:     name,
			Windows:  windows,
			Created:  parts[2],
			Attached: parts[3] == "1",
		})
	}
	return sessions, nil
}

// ListAllSessions returns all sessions including hidden internal ones.
func (b *ExecBridge) ListAllSessions() ([]Session, error) {
	out, err := b.run("list-sessions", "-F",
		"#{session_name}\t#{session_windows}\t#{session_created}\t#{session_attached}")
	if err != nil {
		if strings.Contains(out, "no server running") || strings.Contains(out, "no current") {
			return nil, nil
		}
		return nil, fmt.Errorf("list-sessions: %w: %s", err, out)
	}
	if out == "" {
		return nil, nil
	}

	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		name := parts[0]
		hidden := strings.HasPrefix(name, "workshop-ctrl-") || strings.HasPrefix(name, "consensus-")
		windows, _ := strconv.Atoi(parts[1])
		sessions = append(sessions, Session{
			Name:     name,
			Windows:  windows,
			Created:  parts[2],
			Attached: parts[3] == "1",
			Hidden:   hidden,
		})
	}
	return sessions, nil
}

func (b *ExecBridge) CreateSession(name, startDir string) error {
	args := []string{"new-session", "-d", "-s", name}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	if out, err := b.run(args...); err != nil {
		return fmt.Errorf("new-session: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) RenameSession(oldName, newName string) error {
	if out, err := b.run("rename-session", "-t", oldName, newName); err != nil {
		return fmt.Errorf("rename-session: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) RenameWindow(target, newName string) error {
	if out, err := b.run("rename-window", "-t", target, newName); err != nil {
		return fmt.Errorf("rename-window: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) KillSession(name string) error {
	if out, err := b.run("kill-session", "-t", name); err != nil {
		return fmt.Errorf("kill-session: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) CreateWindow(session, name string) error {
	args := []string{"new-window", "-t", session}
	if name != "" {
		args = append(args, "-n", name)
	}
	if out, err := b.run(args...); err != nil {
		return fmt.Errorf("new-window: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) SplitWindow(target string, horizontal bool) (string, error) {
	flag := "-v"
	if horizontal {
		flag = "-h"
	}
	out, err := b.run("split-window", flag, "-t", target, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("split-window: %w: %s", err, out)
	}
	return out, nil
}

func (b *ExecBridge) SendKeys(target, keys string) error {
	if out, err := b.run("send-keys", "-t", target, keys, "Enter"); err != nil {
		return fmt.Errorf("send-keys: %w: %s", err, out)
	}
	return nil
}

// SendKeysLiteral sends keys literally without appending Enter.
// This is used for raw keystroke forwarding from xterm.js.
func (b *ExecBridge) SendKeysLiteral(target, keys string) error {
	if out, err := b.run("send-keys", "-t", target, "-l", keys); err != nil {
		return fmt.Errorf("send-keys -l: %w: %s", err, out)
	}
	return nil
}

// SendKeysHex sends keys as hex-encoded bytes via tmux send-keys -H.
// This goes through the PTY master so line discipline handles control chars.
func (b *ExecBridge) SendKeysHex(target, hexStr string) error {
	// send-keys -H expects space-separated hex pairs: "68 65 6c 6c 6f"
	spaced := make([]byte, 0, len(hexStr)+len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			spaced = append(spaced, ' ')
		}
		spaced = append(spaced, hexStr[i], hexStr[i+1])
	}
	if out, err := b.run("send-keys", "-t", target, "-H", string(spaced)); err != nil {
		return fmt.Errorf("send-keys -H: %w: %s", err, out)
	}
	return nil
}

func (b *ExecBridge) CapturePane(target string, lines int) (string, error) {
	start := fmt.Sprintf("-%d", lines)
	out, err := b.run("capture-pane", "-t", target, "-p", "-e", "-S", start)
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w: %s", err, out)
	}
	return out, nil
}

// CapturePanePlain captures pane content as plain text (no ANSI escapes).
// Lines are joined with \r\n for xterm.js compatibility.
func (b *ExecBridge) CapturePanePlain(target string, lines int) (string, error) {
	start := fmt.Sprintf("-%d", lines)
	out, err := b.run("capture-pane", "-t", target, "-p", "-S", start)
	if err != nil {
		return "", fmt.Errorf("capture-pane-plain: %w: %s", err, out)
	}
	// Convert \n to \r\n for xterm.js
	return strings.ReplaceAll(out, "\n", "\r\n"), nil
}

// CapturePaneAll captures the entire scrollback history as plain text.
func (b *ExecBridge) CapturePaneAll(target string) (string, error) {
	out, err := b.run("capture-pane", "-t", target, "-p", "-S", "-")
	if err != nil {
		return "", fmt.Errorf("capture-pane-all: %w: %s", err, out)
	}
	return out, nil
}

// ResizePane resizes a tmux pane to the given dimensions.
func (b *ExecBridge) ResizePane(target string, cols, rows int) error {
	if out, err := b.run("resize-pane", "-t", target, "-x", fmt.Sprintf("%d", cols), "-y", fmt.Sprintf("%d", rows)); err != nil {
		return fmt.Errorf("resize-pane: %w: %s", err, out)
	}
	return nil
}

// PaneTTY returns the TTY device path for a pane.
func (b *ExecBridge) PaneTTY(target string) (string, error) {
	out, err := b.run("display-message", "-p", "-t", target, "#{pane_tty}")
	if err != nil {
		return "", fmt.Errorf("pane-tty: %w: %s", err, out)
	}
	return out, nil
}

func (b *ExecBridge) ListPanes(session string) ([]Pane, error) {
	// Use -s flag to list all panes across all windows in the session
	// Trailing colon forces tmux to interpret the target as a session name,
	// not a window index (important when session names are numeric).
	out, err := b.run("list-panes", "-s", "-t", session+":", "-F",
		"#{pane_id}\t#{session_name}:#{window_index}.#{pane_index}\t#{window_name}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_width}\t#{pane_height}\t#{pane_active}")
	if err != nil {
		return nil, fmt.Errorf("list-panes: %w: %s", err, out)
	}
	if out == "" {
		return nil, nil
	}

	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 8)
		if len(parts) < 8 {
			continue
		}
		w, _ := strconv.Atoi(parts[5])
		h, _ := strconv.Atoi(parts[6])
		panes = append(panes, Pane{
			ID:         parts[0],
			Target:     parts[1],
			WindowName: parts[2],
			Command:    parts[3],
			Path:       parts[4],
			Width:      w,
			Height:     h,
			Active:     parts[7] == "1",
		})
	}
	return panes, nil
}
