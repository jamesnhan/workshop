package tmux

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var controlClientCounter atomic.Int64

// CleanupStaleControlSessions kills any leftover workshop-ctrl-* sessions
// from previous runs.
func CleanupStaleControlSessions() {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(name, "workshop-ctrl-") {
			exec.Command("tmux", "kill-session", "-t", name).Run()
		}
	}
}

// ControlClient manages a tmux control mode connection.
// Each browser WebSocket gets its own ControlClient with independent dimensions.
type ControlClient struct {
	session string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	logger  *slog.Logger

	mu          sync.Mutex
	subscribers map[string][]func(data string) // pane ID → callbacks
	cmdID       int
	cmdResults  map[int]chan string
}

// NewControlClient starts a tmux control mode connection to the given session.
// It creates a linked session (workshop-ctrl-{pid}-{session}) so that the control
// client has its own independent size without affecting the original session.
func NewControlClient(session string, logger *slog.Logger) (*ControlClient, error) {
	// Create a linked session with its own size
	id := controlClientCounter.Add(1)
	linkedName := fmt.Sprintf("workshop-ctrl-%d-%s", id, session)
	cmd := exec.Command("tmux", "-C", "new-session", "-t", session, "-s", linkedName)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Discard stderr
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start tmux -C: %w", err)
	}

	cc := &ControlClient{
		session:     session,
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		logger:      logger,
		subscribers: make(map[string][]func(data string)),
		cmdResults:  make(map[int]chan string),
	}

	go cc.readLoop()

	return cc, nil
}

// Subscribe registers a callback for output from a specific pane.
// paneID is the tmux pane ID like "%6".
func (cc *ControlClient) Subscribe(paneID string, cb func(data string)) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.subscribers[paneID] = append(cc.subscribers[paneID], cb)
}

// Unsubscribe removes all callbacks for a pane.
func (cc *ControlClient) Unsubscribe(paneID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	delete(cc.subscribers, paneID)
}

// SetSize sets the virtual client size. Output will be rendered for these dimensions.
func (cc *ControlClient) SetSize(cols, rows int) error {
	return cc.sendCommand(fmt.Sprintf("refresh-client -C '%dx%d'", cols, rows))
}

// SelectWindow switches the control client to view a specific window.
func (cc *ControlClient) SelectWindow(target string) error {
	return cc.sendCommand(fmt.Sprintf("select-window -t '%s'", target))
}

// SendKeys sends input to a pane.
func (cc *ControlClient) SendKeys(paneID, keys string) error {
	// Escape single quotes in the keys
	escaped := strings.ReplaceAll(keys, "'", "'\\''")
	return cc.sendCommand(fmt.Sprintf("send-keys -t '%s' -l '%s'", paneID, escaped))
}

// SendKeyName sends a named key (like "Enter", "Tab", "Left") to a pane.
func (cc *ControlClient) SendKeyName(paneID, keyName string) error {
	return cc.sendCommand(fmt.Sprintf("send-keys -t '%s' %s", paneID, keyName))
}

// SendHex sends hex-encoded bytes to a pane.
func (cc *ControlClient) SendHex(paneID, hex string) error {
	return cc.sendCommand(fmt.Sprintf("send-keys -t '%s' -H %s", paneID, hex))
}

// ResizePane resizes a specific pane.
func (cc *ControlClient) ResizePane(paneID string, cols, rows int) error {
	return cc.sendCommand(fmt.Sprintf("resize-pane -t '%s' -x %d -y %d", paneID, cols, rows))
}

// RefreshClient triggers a redraw by briefly changing size.
func (cc *ControlClient) Redraw(paneID string) error {
	return cc.sendCommand(fmt.Sprintf("send-keys -t '%s' -H 0c", paneID))
}

// Close shuts down the control mode connection and kills the linked session.
func (cc *ControlClient) Close() error {
	cc.sendCommand("kill-session")
	cc.stdin.Close()
	return cc.cmd.Wait()
}

func (cc *ControlClient) sendCommand(cmd string) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	_, err := fmt.Fprintf(cc.stdin, "%s\n", cmd)
	return err
}

// readLoop reads lines from the control mode stdout and dispatches them.
func (cc *ControlClient) readLoop() {
	scanner := bufio.NewScanner(cc.stdout)
	// Increase buffer for large output lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "%output ") {
			cc.handleOutput(line)
		} else if strings.HasPrefix(line, "%begin ") {
			// Skip command response blocks
		} else if strings.HasPrefix(line, "%end ") {
			// Skip
		} else if strings.HasPrefix(line, "%error ") {
			cc.logger.Warn("tmux control error", "line", line)
		} else if strings.HasPrefix(line, "%session-changed") ||
			strings.HasPrefix(line, "%sessions-changed") ||
			strings.HasPrefix(line, "%window-") ||
			strings.HasPrefix(line, "%pane-") ||
			strings.HasPrefix(line, "%layout-change") {
			// Notification — could be useful later
		} else if line == "%exit" {
			cc.logger.Info("tmux control mode exited")
			return
		}
	}
	if err := scanner.Err(); err != nil {
		cc.logger.Warn("control mode read error", "err", err)
	}
}

// handleOutput processes a %output line.
// Format: %output %PANE_ID ESCAPED_DATA
func (cc *ControlClient) handleOutput(line string) {
	// Parse: "%output %6 \033[H\033[J..."
	rest := line[len("%output "):]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return
	}
	paneID := rest[:spaceIdx]
	escapedData := rest[spaceIdx+1:]

	// Unescape octal sequences: \033 → ESC, \015 → CR, \012 → LF, \134 → backslash
	data := unescapeOctal(escapedData)

	cc.mu.Lock()
	cbs := cc.subscribers[paneID]
	cc.mu.Unlock()

	for _, cb := range cbs {
		cb(data)
	}
}

// unescapeOctal converts tmux control mode octal escapes to raw bytes.
// tmux escapes characters < 0x20 and backslash as \NNN (octal).
func unescapeOctal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+3 < len(s) {
			// Check for octal: \NNN where N is 0-7
			d1, d2, d3 := s[i+1], s[i+2], s[i+3]
			if d1 >= '0' && d1 <= '3' && d2 >= '0' && d2 <= '7' && d3 >= '0' && d3 <= '7' {
				val, _ := strconv.ParseUint(string([]byte{d1, d2, d3}), 8, 8)
				b.WriteByte(byte(val))
				i += 4
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
