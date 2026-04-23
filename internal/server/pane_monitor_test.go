package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- detectApproval ---

func TestDetectApproval_RealDialogBox(t *testing.T) {
	// Simulates real approval dialog box lines
	output := `│ ❯ 1. Yes          │`
	assert.True(t, detectApproval(output))
}

func TestDetectApproval_MultipleOptions(t *testing.T) {
	output := "│ ❯ 1. Yes          │\n│   2. No           │"
	assert.True(t, detectApproval(output))
}

func TestDetectApproval_NoDialog(t *testing.T) {
	output := "normal terminal output\njust some code"
	assert.False(t, detectApproval(output))
}

func TestDetectApproval_SelectorWithoutBoxBorders(t *testing.T) {
	// Has ❯ and number but NOT inside box borders — should NOT match
	output := "❯ 1. Some item"
	assert.False(t, detectApproval(output))
}

func TestDetectApproval_BoxBordersWithoutSelector(t *testing.T) {
	// Has box borders but no ❯ selector — should NOT match
	output := "│ Some text         │"
	assert.False(t, detectApproval(output))
}

func TestDetectApproval_ConversationProseAboutDialogs(t *testing.T) {
	// Conversation text mentioning dialog elements shouldn't trigger
	output := `The assistant said: "When you see ❯ 1. in a dialog..."`
	assert.False(t, detectApproval(output))
}

func TestDetectApproval_EmptyOutput(t *testing.T) {
	assert.False(t, detectApproval(""))
}

func TestDetectApproval_LeadingWhitespace(t *testing.T) {
	output := "   │ ❯ 1. Allow once │"
	assert.True(t, detectApproval(output))
}

// --- PaneMonitor.MarkSeen ---

func TestPaneMonitor_MarkSeen_RegistersTarget(t *testing.T) {
	store := NewStatusStore()
	m := NewPaneMonitor(nil, store, nil) // bridge and logger not needed for MarkSeen

	m.MarkSeen("alpha:1.1")

	m.mu.Lock()
	defer m.mu.Unlock()
	assert.True(t, m.knownTargets["alpha:1.1"])
}

func TestPaneMonitor_MarkSeen_Idempotent(t *testing.T) {
	store := NewStatusStore()
	m := NewPaneMonitor(nil, store, nil)

	m.MarkSeen("alpha:1.1")
	m.MarkSeen("alpha:1.1") // should not panic or double-register

	m.mu.Lock()
	defer m.mu.Unlock()
	assert.True(t, m.knownTargets["alpha:1.1"])
}
