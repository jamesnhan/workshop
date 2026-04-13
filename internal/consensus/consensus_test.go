package consensus

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Minimal tmux.Bridge stub ---

// stubBridge is a no-op Bridge used only by tests that exercise code
// paths which don't actually touch the bridge (e.g. StartRun validation,
// pure helpers). Methods that shouldn't be called return errors if they
// are, so we notice accidental goroutine leaks.
type stubBridge struct{}

var _ tmux.Bridge = (*stubBridge)(nil)

func (stubBridge) ListSessions() ([]tmux.Session, error)                    { return nil, nil }
func (stubBridge) CreateSession(_, _ string) error                          { return nil }
func (stubBridge) KillSession(_ string) error                               { return nil }
func (stubBridge) RenameSession(_, _ string) error                          { return nil }
func (stubBridge) RenameWindow(_, _ string) error                           { return nil }
func (stubBridge) CreateWindow(_, _ string) error                           { return nil }
func (stubBridge) SplitWindow(_ string, _ bool) (string, error)             { return "", nil }
func (stubBridge) SendKeys(_, _ string) error                               { return nil }
func (stubBridge) SendKeysLiteral(_, _ string) error                        { return nil }
func (stubBridge) SendKeysHex(_, _ string) error                            { return nil }
func (stubBridge) SendInput(_, _ string) error                              { return nil }
func (stubBridge) CapturePane(_ string, _ int) (string, error)              { return "", nil }
func (stubBridge) CapturePanePlain(_ string, _ int) (string, error)         { return "", nil }
func (stubBridge) CapturePaneVisible(_ string) (string, error)              { return "", nil }
func (stubBridge) CapturePaneAll(_ string) (string, error)                  { return "", nil }
func (stubBridge) RunRaw(_ ...string) (string, error)                       { return "", nil }
func (stubBridge) ResizePane(_ string, _, _ int) error                      { return nil }
func (stubBridge) PaneTTY(_ string) (string, error)                         { return "", nil }
func (stubBridge) ListPanes(_ string) ([]tmux.Pane, error)                  { return nil, nil }
func (stubBridge) LaunchAgent(_ tmux.AgentConfig) (*tmux.AgentResult, error) {
	return nil, errors.New("stubBridge.LaunchAgent should not be called in these tests")
}

// newEngine builds a fresh engine with a discarding logger, no DB, and a
// stub bridge. Tests that need the bridge or db plug them in explicitly.
func newEngine(t *testing.T) *Engine {
	t.Helper()
	return NewEngine(stubBridge{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// --- StartRun validation ---

func TestStartRun_rejectsEmptyAgents(t *testing.T) {
	e := newEngine(t)
	_, err := e.StartRun(ConsensusRequest{Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one agent")
}

func TestStartRun_rejectsEmptyPrompt(t *testing.T) {
	e := newEngine(t)
	_, err := e.StartRun(ConsensusRequest{
		Agents: []AgentSpec{{Name: "a", Model: "opus"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestStartRun_defaultsTimeoutTo300(t *testing.T) {
	// We can't observe the timeout field through the public API, but we
	// can at least confirm StartRun succeeds with Timeout left zero and
	// places the run in the in-memory map. The launched goroutine calls
	// stubBridge.LaunchAgent which returns an error, so the run ends in
	// an "error" state — that's fine; what we're asserting is that the
	// request-validation path accepted the zero timeout.
	e := newEngine(t)
	res, err := e.StartRun(ConsensusRequest{
		Prompt: "x",
		Agents: []AgentSpec{{Name: "a", Model: "opus"}},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.ID)
	assert.Equal(t, "running", res.Status)
}

// --- GetRun / ListRuns ---

func TestGetRun_unknownReturnsNil(t *testing.T) {
	e := newEngine(t)
	assert.Nil(t, e.GetRun("no-such-id"))
}

func TestListRuns_includesActive(t *testing.T) {
	e := newEngine(t)
	res, err := e.StartRun(ConsensusRequest{
		Prompt: "x",
		Agents: []AgentSpec{{Name: "a", Model: "opus"}},
	})
	require.NoError(t, err)

	runs := e.ListRuns()
	found := false
	for _, r := range runs {
		if r.ID == res.ID {
			found = true
		}
	}
	assert.True(t, found, "ListRuns should include in-memory runs")
}

// --- setStatus ---

func TestSetStatus_updatesInMemory(t *testing.T) {
	e := newEngine(t)
	res, err := e.StartRun(ConsensusRequest{
		Prompt: "x",
		Agents: []AgentSpec{{Name: "a", Model: "opus"}},
	})
	require.NoError(t, err)

	e.setStatus(res.ID, "collecting")
	got := e.GetRun(res.ID)
	require.NotNil(t, got)
	assert.Equal(t, "collecting", got.Status)
}

// --- completionPatternsForProvider ---

func TestCompletionPatterns_perProvider(t *testing.T) {
	claude := completionPatternsForProvider(tmux.ProviderClaude)
	gemini := completionPatternsForProvider(tmux.ProviderGemini)
	codex := completionPatternsForProvider(tmux.ProviderCodex)

	assert.Contains(t, claude, "worked for")
	assert.Contains(t, claude, "baked for")
	assert.Equal(t, []string{"✦"}, gemini)
	assert.Contains(t, codex, "completed in")
	assert.Contains(t, codex, "done in")

	// Default / unknown provider → claude patterns.
	assert.Equal(t, claude, completionPatternsForProvider(""))
	assert.Equal(t, claude, completionPatternsForProvider("unknown"))
}

// --- buildCoordinatorPrompt ---

func TestBuildCoordinatorPrompt_includesOriginalAndEveryAgent(t *testing.T) {
	e := newEngine(t)
	outputs := []AgentOutput{
		{Name: "alpha", Model: "opus", Status: "completed", Output: "alpha result"},
		{Name: "beta", Model: "sonnet", Status: "completed", Output: "beta result"},
	}
	prompt := e.buildCoordinatorPrompt("Summarize the codebase", outputs)

	assert.Contains(t, prompt, "Summarize the codebase")
	assert.Contains(t, prompt, "Agent: alpha")
	assert.Contains(t, prompt, "alpha result")
	assert.Contains(t, prompt, "Agent: beta")
	assert.Contains(t, prompt, "beta result")
	assert.Contains(t, prompt, "model: opus")
	assert.Contains(t, prompt, "model: sonnet")
}

func TestBuildCoordinatorPrompt_truncatesVeryLongOutputs(t *testing.T) {
	e := newEngine(t)
	long := strings.Repeat("x", 8000)
	outputs := []AgentOutput{
		{Name: "a", Model: "opus", Status: "completed", Output: long},
	}
	prompt := e.buildCoordinatorPrompt("q", outputs)
	assert.Contains(t, prompt, "(truncated)")
	// Truncated to 5000 + ellipsis, so the original 8000-char blob shouldn't
	// be present verbatim.
	assert.NotContains(t, prompt, long)
}

func TestBuildCoordinatorPrompt_handlesMissingOutput(t *testing.T) {
	e := newEngine(t)
	outputs := []AgentOutput{
		{Name: "crashed", Model: "opus", Status: "error", Output: ""},
	}
	prompt := e.buildCoordinatorPrompt("q", outputs)
	assert.Contains(t, prompt, "(no output)")
}
