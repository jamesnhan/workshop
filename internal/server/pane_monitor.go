package server

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jamesnhan/workshop/internal/tmux"
)

// PaneMonitor polls every tmux pane in the background and auto-sets a yellow
// status when it detects an approval/permission dialog. This surfaces pending
// approvals on unfocused tabs where the agent itself cannot call set_pane_status
// (because it's blocked waiting for user input).
type PaneMonitor struct {
	bridge       tmux.Bridge
	store        *StatusStore
	logger       *slog.Logger
	interval     time.Duration
	mu           sync.Mutex
	autoOwned    map[string]bool // targets whose current yellow was set by us
	knownTargets map[string]bool // targets seen in a prior scan (for session_created detection)
	initialized  bool            // first scan completed (seeded knownTargets without broadcasting)
}

func NewPaneMonitor(bridge tmux.Bridge, store *StatusStore, logger *slog.Logger) *PaneMonitor {
	return &PaneMonitor{
		bridge:       bridge,
		store:        store,
		logger:       logger,
		interval:     3 * time.Second,
		autoOwned:    make(map[string]bool),
		knownTargets: make(map[string]bool),
	}
}

// MarkSeen pre-registers a target so the monitor doesn't broadcast a
// session_created event for it. Callers that already broadcast the
// event themselves (e.g. REST handlers with background=false so the
// frontend focuses the new pane) should call this first to prevent a
// duplicate background-tab broadcast on the next scan.
func (m *PaneMonitor) MarkSeen(target string) {
	m.mu.Lock()
	m.knownTargets[target] = true
	m.mu.Unlock()
}

// Start runs the monitor loop in a goroutine until the stop channel is closed.
func (m *PaneMonitor) Start(stop <-chan struct{}) {
	go m.loop(stop)
}

func (m *PaneMonitor) loop(stop <-chan struct{}) {
	tick := time.NewTicker(m.interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			m.scan()
		}
	}
}

func (m *PaneMonitor) scan() {
	sessions, err := m.bridge.ListSessions()
	if err != nil {
		return
	}
	// Snapshot current statuses once per scan.
	current := m.store.GetAll()

	seen := make(map[string]bool)
	var newTargets []newTarget

	for _, s := range sessions {
		// Skip internal/hidden sessions — workshop-ctrl-*
		if strings.HasPrefix(s.Name, "workshop-ctrl-") {
			continue
		}
		panes, err := m.bridge.ListPanes(s.Name)
		if err != nil {
			continue
		}
		for _, p := range panes {
			seen[p.Target] = true
			m.checkPane(p.Target, current)
			m.mu.Lock()
			known := m.knownTargets[p.Target]
			m.knownTargets[p.Target] = true
			m.mu.Unlock()
			if !known && m.initialized {
				newTargets = append(newTargets, newTarget{target: p.Target, session: s.Name})
			}
		}
	}

	// Broadcast session_created for any targets that weren't seen before.
	// These default to background=true — the only way a target reaches
	// this path without being pre-registered via MarkSeen is via direct
	// bridge calls (MCP, external tmux), none of which are user-initiated
	// in-UI actions.
	for _, nt := range newTargets {
		m.store.Broadcast("session_created", map[string]any{
			"target":     nt.target,
			"session":    nt.session,
			"background": true,
		})
	}

	// Clear auto-owned statuses and known-targets entries for panes that no longer exist.
	m.mu.Lock()
	for target := range m.autoOwned {
		if !seen[target] {
			delete(m.autoOwned, target)
			m.store.Clear(target)
		}
	}
	for target := range m.knownTargets {
		if !seen[target] {
			delete(m.knownTargets, target)
		}
	}
	m.initialized = true
	m.mu.Unlock()
}

type newTarget struct {
	target  string
	session string
}

func (m *PaneMonitor) checkPane(target string, current map[string]PaneStatus) {
	// Capture only the visible screen — scrollback contains historical
	// conversation text that can contain approval-dialog phrases verbatim
	// (e.g. the assistant discussing approval dialogs), which would cause
	// false positives. The real dialog is always in the live viewport.
	out, err := m.bridge.CapturePaneVisible(target)
	if err != nil {
		return
	}
	needsApproval := detectApproval(out)

	m.mu.Lock()
	owned := m.autoOwned[target]
	m.mu.Unlock()

	existing, hasExisting := current[target]

	if needsApproval {
		// If someone else set a manual status, don't overwrite it.
		if hasExisting && !owned {
			return
		}
		if owned && existing.Status == "yellow" {
			return // already set
		}
		m.mu.Lock()
		m.autoOwned[target] = true
		m.mu.Unlock()
		m.store.Set(target, "yellow", "Needs approval")
		return
	}

	// No approval detected — clear if we were the ones who set it.
	if owned {
		m.mu.Lock()
		delete(m.autoOwned, target)
		m.mu.Unlock()
		// Only clear if the current status is still our yellow.
		if hasExisting && existing.Status == "yellow" && existing.Message == "Needs approval" {
			m.store.Clear(target)
		}
	}
}

// dialogSelectorLine matches a single line where the ❯ selector points at
// a numbered option AND the line both begins and ends with the box-border
// character │ (allowing trailing whitespace). This precisely identifies a
// row inside an active approval dialog box — e.g. "│ ❯ 1. Yes          │".
//
// Conversation prose can contain "❯ 1. " and "│" independently, but those
// characters won't co-occur as the opening and closing of a single line.
// The real dialog always renders them as box structure.
var dialogSelectorLine = regexp.MustCompile(`(?m)^\s*│.*❯\s*\d+\.\s.*│\s*$`)

// detectApproval returns true if the captured visible pane content looks
// like an approval/permission dialog waiting for user input.
func detectApproval(out string) bool {
	return dialogSelectorLine.MatchString(out)
}
