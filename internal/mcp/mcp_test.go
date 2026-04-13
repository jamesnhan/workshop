package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

// callTool invokes a handler with a synthetic request carrying the given
// arguments and returns the result. Handlers never return a non-nil error
// (they wrap errors in NewToolResultError), so we don't fail on err here.
func callTool(t *testing.T, h func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	require.NoError(t, err)
	return res
}

// isError returns true if the tool result is flagged as an error.
func isError(res *mcp.CallToolResult) bool {
	return res != nil && res.IsError
}

// resultText concatenates all text content in the result. Used to assert
// on happy-path output.
func resultText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// --- Fake tmux bridge ---

// fakeBridge records every call and lets tests seed return values. Only the
// subset of the Bridge interface exercised by the MCP handlers needs to do
// anything meaningful — the rest panic so we notice if they get hit.
type fakeBridge struct {
	mu sync.Mutex

	sessions []tmux.Session
	panes    []tmux.Pane

	sendKeysCalls         []fakeBridgeCall
	sendKeysLiteralCalls  []fakeBridgeCall
	createSessionCalls    []fakeBridgeCall
	killSessionCalls      []string
	renameSessionCalls    [][2]string
	createWindowCalls     []fakeBridgeCall
	splitWindowCalls      []fakeBridgeCall
	capturePaneCalls      []fakeBridgeCall
	capturePaneOutput     string

	listSessionsErr error
}

type fakeBridgeCall struct {
	A string
	B string
}

var _ tmux.Bridge = (*fakeBridge)(nil)

func (f *fakeBridge) ListSessions() ([]tmux.Session, error) {
	return f.sessions, f.listSessionsErr
}
func (f *fakeBridge) CreateSession(name, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createSessionCalls = append(f.createSessionCalls, fakeBridgeCall{A: name, B: dir})
	return nil
}
func (f *fakeBridge) KillSession(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killSessionCalls = append(f.killSessionCalls, name)
	return nil
}
func (f *fakeBridge) RenameSession(oldName, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renameSessionCalls = append(f.renameSessionCalls, [2]string{oldName, newName})
	return nil
}
func (f *fakeBridge) RenameWindow(_, _ string) error { return nil }
func (f *fakeBridge) CreateWindow(session, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createWindowCalls = append(f.createWindowCalls, fakeBridgeCall{A: session, B: name})
	return nil
}
func (f *fakeBridge) SplitWindow(target string, horizontal bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.splitWindowCalls = append(f.splitWindowCalls, fakeBridgeCall{A: target, B: fmt.Sprintf("%v", horizontal)})
	return "%new-pane", nil
}
func (f *fakeBridge) SendKeys(target, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendKeysCalls = append(f.sendKeysCalls, fakeBridgeCall{A: target, B: keys})
	return nil
}
func (f *fakeBridge) SendKeysLiteral(target, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendKeysLiteralCalls = append(f.sendKeysLiteralCalls, fakeBridgeCall{A: target, B: keys})
	return nil
}
func (f *fakeBridge) SendKeysHex(_, _ string) error { return nil }
func (f *fakeBridge) SendInput(_, _ string) error   { return nil }
func (f *fakeBridge) CapturePane(target string, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.capturePaneCalls = append(f.capturePaneCalls, fakeBridgeCall{A: target, B: fmt.Sprintf("%d", lines)})
	return f.capturePaneOutput, nil
}
func (f *fakeBridge) CapturePanePlain(_ string, _ int) (string, error) { return "", nil }
func (f *fakeBridge) CapturePaneVisible(_ string) (string, error)      { return "", nil }
func (f *fakeBridge) CapturePaneAll(_ string) (string, error)          { return "", nil }
func (f *fakeBridge) RunRaw(_ ...string) (string, error)               { return "", nil }
func (f *fakeBridge) ResizePane(_ string, _, _ int) error              { return nil }
func (f *fakeBridge) PaneTTY(_ string) (string, error)                 { return "", nil }
func (f *fakeBridge) ListPanes(_ string) ([]tmux.Pane, error) {
	return f.panes, nil
}
func (f *fakeBridge) LaunchAgent(_ tmux.AgentConfig) (*tmux.AgentResult, error) {
	return nil, errors.New("not used in tests")
}

// --- Bridge-backed handlers ---

func TestListSessionsHandler_returnsBridgeSessions(t *testing.T) {
	b := &fakeBridge{sessions: []tmux.Session{{Name: "alpha", Windows: 1, Attached: true}}}
	res := callTool(t, listSessionsHandler(b), nil)
	require.False(t, isError(res))

	var got []tmux.Session
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &got))
	assert.Len(t, got, 1)
	assert.Equal(t, "alpha", got[0].Name)
}

func TestListSessionsHandler_propagatesBridgeError(t *testing.T) {
	b := &fakeBridge{listSessionsErr: errors.New("boom")}
	res := callTool(t, listSessionsHandler(b), nil)
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "boom")
}

func TestListPanesHandler_requiresSession(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, listPanesHandler(b), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "session is required")
}

func TestListPanesHandler_happyPath(t *testing.T) {
	b := &fakeBridge{panes: []tmux.Pane{{Target: "a:1.1"}}}
	res := callTool(t, listPanesHandler(b), map[string]any{"session": "a"})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "a:1.1")
}

func TestKillSessionHandler_requiresName(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, killSessionHandler(b), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "name is required")
	assert.Empty(t, b.killSessionCalls)
}

func TestKillSessionHandler_happyPath(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, killSessionHandler(b), map[string]any{"name": "alpha"})
	assert.False(t, isError(res))
	assert.Equal(t, []string{"alpha"}, b.killSessionCalls)
}

func TestSendKeysHandler_requiresTargetAndCommand(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, sendKeysHandler(b), map[string]any{"target": "a:1.1"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "target and command are required")
	assert.Empty(t, b.sendKeysCalls)
}

func TestSendKeysHandler_happyPath(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, sendKeysHandler(b), map[string]any{"target": "a:1.1", "command": "ls"})
	assert.False(t, isError(res))
	require.Len(t, b.sendKeysCalls, 1)
	assert.Equal(t, "a:1.1", b.sendKeysCalls[0].A)
	assert.Equal(t, "ls", b.sendKeysCalls[0].B)
}

func TestSendTextHandler_requiresTargetAndText(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, sendTextHandler(b), map[string]any{"target": "a:1.1"})
	assert.True(t, isError(res))
}

func TestSendTextHandler_happyPath(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, sendTextHandler(b), map[string]any{"target": "a:1.1", "text": "hello"})
	assert.False(t, isError(res))
	require.Len(t, b.sendKeysLiteralCalls, 1)
	assert.Equal(t, "hello", b.sendKeysLiteralCalls[0].B)
}

func TestCapturePaneHandler_requiresTarget(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, capturePaneHandler(b), map[string]any{})
	assert.True(t, isError(res))
}

func TestCapturePaneHandler_stripsAnsi(t *testing.T) {
	// stripAnsi is used by the handler — feed ANSI-coloured content and
	// assert the escape sequences are gone from the output.
	b := &fakeBridge{capturePaneOutput: "\x1b[31mred\x1b[0m plain"}
	res := callTool(t, capturePaneHandler(b), map[string]any{"target": "a:1.1"})
	assert.False(t, isError(res))
	text := resultText(res)
	assert.NotContains(t, text, "\x1b[")
	assert.Contains(t, text, "red")
	assert.Contains(t, text, "plain")
}

func TestSplitWindowHandler_requiresTarget(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, splitWindowHandler(b), map[string]any{})
	assert.True(t, isError(res))
}

func TestSplitWindowHandler_happyPath(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, splitWindowHandler(b), map[string]any{"target": "a:1", "horizontal": true})
	assert.False(t, isError(res))
	require.Len(t, b.splitWindowCalls, 1)
	assert.Equal(t, "a:1", b.splitWindowCalls[0].A)
	assert.Equal(t, "true", b.splitWindowCalls[0].B)
}

func TestCreateWindowHandler_requiresSession(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, createWindowHandler(b), map[string]any{})
	assert.True(t, isError(res))
}

func TestRenameSessionHandler_requiresBothNames(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, renameSessionHandler(b), map[string]any{"name": "old"})
	assert.True(t, isError(res))
	res = callTool(t, renameSessionHandler(b), map[string]any{"old_name": "old", "new_name": "new"})
	assert.False(t, isError(res))
	assert.Equal(t, [2]string{"old", "new"}, b.renameSessionCalls[0])
}

// --- HTTP-backed handlers ---

// withFakeAPI spins up an httptest.Server, points WORKSHOP_API_URL at it, and
// registers cleanup. Returns a mux so tests can wire per-endpoint handlers.
func withFakeAPI(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Setenv("WORKSHOP_API_URL", srv.URL)
	t.Cleanup(srv.Close)
	return mux
}

func TestSetPaneStatusHandler_requiresTargetAndStatus(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, setPaneStatusHandler(), map[string]any{"target": "a:1.1"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "target and status are required")
}

func TestSetPaneStatusHandler_postsToAPI(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]string
	mux.HandleFunc("/api/v1/panes/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, setPaneStatusHandler(), map[string]any{
		"target": "a:1.1", "status": "green", "message": "done",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "a:1.1", received["target"])
	assert.Equal(t, "green", received["status"])
	assert.Equal(t, "done", received["message"])
}

func TestSetPaneStatusHandler_apiErrorReturnsToolError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/panes/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	})
	res := callTool(t, setPaneStatusHandler(), map[string]any{
		"target": "a:1.1", "status": "green",
	})
	assert.True(t, isError(res))
}

func TestClearPaneStatusHandler_requiresTarget(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, clearPaneStatusHandler(), map[string]any{})
	assert.True(t, isError(res))
}

func TestClearPaneStatusHandler_callsDelete(t *testing.T) {
	mux := withFakeAPI(t)
	var method string
	mux.HandleFunc("/api/v1/panes/status", func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusOK)
	})
	res := callTool(t, clearPaneStatusHandler(), map[string]any{"target": "a:1.1"})
	assert.False(t, isError(res))
	assert.Equal(t, http.MethodDelete, method)
}

// --- Kanban handlers ---

func TestKanbanCreateHandler_requiresTitle(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, kanbanCreateHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, strings.ToLower(resultText(res)), "title")
}

func TestKanbanCreateHandler_postsCard(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":99,"title":"hello"}`))
	})
	res := callTool(t, kanbanCreateHandler(), map[string]any{
		"title":    "hello",
		"project":  "workshop",
		"priority": "P1",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "hello", received["title"])
	assert.Equal(t, "workshop", received["project"])
}

func TestKanbanListHandler_fetchesProject(t *testing.T) {
	mux := withFakeAPI(t)
	var gotProject string
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		gotProject = r.URL.Query().Get("project")
		_, _ = w.Write([]byte(`[]`))
	})
	res := callTool(t, kanbanListHandler(), map[string]any{"project": "workshop"})
	assert.False(t, isError(res))
	assert.Equal(t, "workshop", gotProject)
}

// --- Channel handlers ---

func TestChannelPublishHandler_postsPayload(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/channels/publish", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	res := callTool(t, channelPublishHandler(), map[string]any{
		"channel": "room",
		"body":    "hi",
		"from":    "alpha",
		"project": "p",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "room", received["channel"])
	assert.Equal(t, "hi", received["body"])
	assert.Equal(t, "alpha", received["from"])
}

func TestChannelSubscribeHandler_postsPayload(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/channels/subscribe", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	res := callTool(t, channelSubscribeHandler(), map[string]any{
		"channel": "room",
		"target":  "alpha:1.1",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "room", received["channel"])
	assert.Equal(t, "alpha:1.1", received["target"])
}
