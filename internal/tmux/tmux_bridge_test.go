package tmux

import (
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedRunner is a richer CommandRunner stub than the one in tmux_test.go:
// it records every invocation, and lets tests script stdout + error state
// per tmux subcommand (e.g. "list-sessions", "new-session"). The existing
// fakeCmd helper handles a single command; scriptedRunner covers the full
// bridge where many methods share a bridge instance.
type scriptedRunner struct {
	mu sync.Mutex

	calls    []scriptedCall
	outputs  map[string]string
	errorFor map[string]bool
}

type scriptedCall struct {
	Name string
	Args []string
}

func newScripted() *scriptedRunner {
	return &scriptedRunner{
		outputs:  map[string]string{},
		errorFor: map[string]bool{},
	}
}

func (s *scriptedRunner) runner() CommandRunner {
	return func(name string, args ...string) *exec.Cmd {
		s.mu.Lock()
		s.calls = append(s.calls, scriptedCall{Name: name, Args: append([]string(nil), args...)})
		sub := ""
		if len(args) > 0 {
			sub = args[0]
		}
		out := s.outputs[sub]
		fail := s.errorFor[sub]
		s.mu.Unlock()

		if fail {
			// Emit scripted output to stderr and exit 1 so CombinedOutput
			// returns the text alongside the error.
			script := "printf %s \"$1\" 1>&2; exit 1"
			return exec.Command("sh", "-c", script, "--", out)
		}
		return exec.Command("printf", "%s", out)
	}
}

func bridgeWith() (*ExecBridge, *scriptedRunner) {
	s := newScripted()
	return &ExecBridge{tmuxPath: "tmux", runCmd: s.runner()}, s
}

// --- ListSessions: hidden filtering ---

func TestListSessions_hidesInternalSessions(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["list-sessions"] = strings.Join([]string{
		"alpha\t1\t0\t0",
		"workshop-ctrl-abc\t1\t0\t0",
		"consensus-xyz\t1\t0\t0",
		"beta\t1\t0\t0",
	}, "\n")

	sessions, err := b.ListSessions()
	require.NoError(t, err)
	names := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		names = append(names, sess.Name)
	}
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestListAllSessions_includesHidden(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["list-sessions"] = strings.Join([]string{
		"alpha\t1\t0\t0",
		"workshop-ctrl-abc\t1\t0\t0",
		"consensus-xyz\t1\t0\t0",
	}, "\n")

	sessions, err := b.ListAllSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 3)

	hidden := map[string]bool{}
	for _, sess := range sessions {
		hidden[sess.Name] = sess.Hidden
	}
	assert.False(t, hidden["alpha"])
	assert.True(t, hidden["workshop-ctrl-abc"])
	assert.True(t, hidden["consensus-xyz"])
}

// --- CreateSession ---

func TestCreateSession_noStartDir(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.CreateSession("alpha", ""))
	require.Len(t, s.calls, 1)
	assert.Equal(t, []string{"new-session", "-d", "-s", "alpha"}, s.calls[0].Args)
}

func TestCreateSession_withStartDir(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.CreateSession("alpha", "/home/me/repo"))
	require.Len(t, s.calls, 1)
	assert.Equal(t, []string{"new-session", "-d", "-s", "alpha", "-c", "/home/me/repo"}, s.calls[0].Args)
}

func TestCreateSession_propagatesError(t *testing.T) {
	b, s := bridgeWith()
	s.errorFor["new-session"] = true
	s.outputs["new-session"] = "duplicate session"

	err := b.CreateSession("alpha", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "new-session")
	assert.Contains(t, err.Error(), "duplicate session")
}

// --- KillSession / RenameSession / RenameWindow ---

func TestKillSession_callsTmuxKillSession(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.KillSession("alpha"))
	assert.Equal(t, []string{"kill-session", "-t", "alpha"}, s.calls[0].Args)
}

func TestKillSession_errorIncludesOutput(t *testing.T) {
	b, s := bridgeWith()
	s.errorFor["kill-session"] = true
	s.outputs["kill-session"] = "session not found"

	err := b.KillSession("ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
}

func TestRenameSession(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.RenameSession("old", "new"))
	assert.Equal(t, []string{"rename-session", "-t", "old", "new"}, s.calls[0].Args)
}

func TestRenameWindow(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.RenameWindow("alpha:1", "editor"))
	assert.Equal(t, []string{"rename-window", "-t", "alpha:1", "editor"}, s.calls[0].Args)
}

// --- Windows / splits ---

func TestCreateWindow_withName(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.CreateWindow("alpha", "editor"))
	assert.Equal(t, []string{"new-window", "-t", "alpha", "-n", "editor"}, s.calls[0].Args)
}

func TestCreateWindow_withoutName(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.CreateWindow("alpha", ""))
	assert.Equal(t, []string{"new-window", "-t", "alpha"}, s.calls[0].Args)
}

func TestSplitWindow_vertical(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["split-window"] = "%12"

	pane, err := b.SplitWindow("alpha:1", false)
	require.NoError(t, err)
	assert.Equal(t, "%12", pane)
	assert.Contains(t, s.calls[0].Args, "-v")
}

func TestSplitWindow_horizontal(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["split-window"] = "%13"

	_, err := b.SplitWindow("alpha:1", true)
	require.NoError(t, err)
	assert.Contains(t, s.calls[0].Args, "-h")
}

// --- Send keys ---

func TestSendKeys_appendsEnter(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.SendKeys("alpha:1.1", "ls -la"))
	assert.Equal(t, []string{"send-keys", "-t", "alpha:1.1", "ls -la", "Enter"}, s.calls[0].Args)
}

func TestSendKeysLiteral_passesLiteralFlag(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.SendKeysLiteral("alpha:1.1", "raw bytes"))
	assert.Equal(t, []string{"send-keys", "-t", "alpha:1.1", "-l", "raw bytes"}, s.calls[0].Args)
}

func TestSendKeysHex_spacedHexPairs(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.SendKeysHex("alpha:1.1", "68656c6c6f"))
	require.Len(t, s.calls, 1)
	args := s.calls[0].Args
	require.Len(t, args, 5)
	assert.Equal(t, "-H", args[3])
	assert.Equal(t, "68 65 6c 6c 6f", args[4])
}

// --- Capture ---

func TestCapturePane_requestsScrollback(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["capture-pane"] = "line1\nline2"

	out, err := b.CapturePane("alpha:1.1", 100)
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2", out)
	assert.Contains(t, s.calls[0].Args, "-S")
	assert.Contains(t, s.calls[0].Args, "-100")
}

func TestCapturePanePlain_convertsNewlinesForXterm(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["capture-pane"] = "line1\nline2"

	out, err := b.CapturePanePlain("alpha:1.1", 10)
	require.NoError(t, err)
	assert.Equal(t, "line1\r\nline2", out)
}

func TestCapturePaneVisible_noScrollbackFlag(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["capture-pane"] = "current"

	out, err := b.CapturePaneVisible("alpha:1.1")
	require.NoError(t, err)
	assert.Equal(t, "current", out)
	for _, a := range s.calls[0].Args {
		assert.NotEqual(t, "-S", a)
	}
}

func TestCapturePaneAll_requestsFullScrollback(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["capture-pane"] = "all history"

	_, err := b.CapturePaneAll("alpha:1.1")
	require.NoError(t, err)
	assert.Contains(t, s.calls[0].Args, "-S")
	assert.Contains(t, s.calls[0].Args, "-")
}

// --- Resize / Pane listing ---

func TestResizePane_passesCorrectFlags(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.ResizePane("alpha:1.1", 120, 40))
	args := s.calls[0].Args
	assert.Equal(t, "resize-pane", args[0])
	assert.Contains(t, args, "-x")
	assert.Contains(t, args, "120")
	assert.Contains(t, args, "-y")
	assert.Contains(t, args, "40")
}

func TestListPanes_parsesTabDelimitedOutput(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["list-panes"] = strings.Join([]string{
		"%1\talpha:0.0\teditor\tnvim\t/home/me\t120\t40\t1",
		"%2\talpha:0.1\tshell\tzsh\t/tmp\t80\t24\t0",
	}, "\n")

	panes, err := b.ListPanes("alpha")
	require.NoError(t, err)
	require.Len(t, panes, 2)
	assert.Equal(t, "%1", panes[0].ID)
	assert.Equal(t, "alpha:0.0", panes[0].Target)
	assert.Equal(t, "editor", panes[0].WindowName)
	assert.Equal(t, "nvim", panes[0].Command)
	assert.Equal(t, "/home/me", panes[0].Path)
	assert.Equal(t, 120, panes[0].Width)
	assert.Equal(t, 40, panes[0].Height)
	assert.True(t, panes[0].Active)
	assert.False(t, panes[1].Active)
}

func TestListPanes_sessionArgHasTrailingColon(t *testing.T) {
	// Trailing colon ensures tmux interprets numeric session names as
	// sessions, not window indices.
	b, s := bridgeWith()
	s.outputs["list-panes"] = ""

	_, err := b.ListPanes("123")
	require.NoError(t, err)
	require.Len(t, s.calls, 1)
	args := s.calls[0].Args
	idx := -1
	for i, a := range args {
		if a == "-t" {
			idx = i + 1
			break
		}
	}
	require.GreaterOrEqual(t, idx, 0, "-t argument must be present")
	assert.Equal(t, "123:", args[idx])
}

func TestPaneTTY_returnsTmuxOutput(t *testing.T) {
	b, s := bridgeWith()
	s.outputs["display-message"] = "/dev/pts/42"

	tty, err := b.PaneTTY("alpha:1.1")
	require.NoError(t, err)
	assert.Equal(t, "/dev/pts/42", tty)
}
