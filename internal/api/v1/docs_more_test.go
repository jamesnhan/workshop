package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStatus is a minimal StatusManager stub that records broadcast calls.
// handleOpenDoc calls Broadcast; the other methods are unused in tests but
// must exist to satisfy the interface.
type fakeStatus struct {
	mu    sync.Mutex
	calls []fakeBroadcast
}

type fakeBroadcast struct {
	Type string
	Data any
}

func (f *fakeStatus) Set(_, _, _ string)        {}
func (f *fakeStatus) Clear(_ string)            {}
func (f *fakeStatus) MarkSeen(_ string)         {}
func (f *fakeStatus) Broadcast(t string, d any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeBroadcast{Type: t, Data: d})
}

func (f *fakeStatus) Broadcasts() []fakeBroadcast {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeBroadcast(nil), f.calls...)
}

// newDocsAPI constructs an API with a discarding logger and fake status for
// the docs handlers.
func newDocsAPI(t *testing.T) (*API, *fakeStatus) {
	t.Helper()
	fs := &fakeStatus{}
	return &API{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		status: fs,
	}, fs
}

// tempDocInHome creates a file under a tempdir inside $HOME (scoped by the
// test) and returns its absolute path. Uses os.CreateTemp so the file is
// uniquely named per test and cleaned up on Cleanup.
func tempDocInHome(t *testing.T, name, content string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	dir, err := os.MkdirTemp(home, "workshop-docs-more-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return path
}

// --- handleReadFile: content roundtrip + ext variants ---

func TestReadFile_contentRoundtrip(t *testing.T) {
	api, _ := newDocsAPI(t)
	path := tempDocInHome(t, "note.md", "# hello\nworld")

	req := httptest.NewRequest(http.MethodGet, "/docs/read?path="+url.QueryEscape(path), nil)
	w := httptest.NewRecorder()
	api.handleReadFile(w, req)

	res := w.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	assert.Equal(t, "# hello\nworld", body["content"])
	assert.Equal(t, "note.md", body["name"])
}

func TestReadFile_allowedExtensions(t *testing.T) {
	api, _ := newDocsAPI(t)
	// Extensions whitelisted in docs.go.
	for _, ext := range []string{".md", ".txt", ".yaml", ".yml", ".json", ".lua", ".toml"} {
		path := tempDocInHome(t, "file"+ext, "body")
		req := httptest.NewRequest(http.MethodGet, "/docs/read?path="+url.QueryEscape(path), nil)
		w := httptest.NewRecorder()
		api.handleReadFile(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "ext %s should be allowed", ext)
	}
}

func TestReadFile_404ForMissingAllowedFile(t *testing.T) {
	api, _ := newDocsAPI(t)
	home, _ := os.UserHomeDir()
	missing := filepath.Join(home, "workshop-docs-test-missing.md")
	req := httptest.NewRequest(http.MethodGet, "/docs/read?path="+url.QueryEscape(missing), nil)
	w := httptest.NewRecorder()
	api.handleReadFile(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- handleOpenDoc ---

func TestOpenDoc_broadcastsPath(t *testing.T) {
	api, fs := newDocsAPI(t)
	path := tempDocInHome(t, "pushed.md", "content")

	reqBody, _ := json.Marshal(map[string]string{"path": path})
	req := httptest.NewRequest(http.MethodPost, "/docs/open", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	api.handleOpenDoc(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	broadcasts := fs.Broadcasts()
	require.Len(t, broadcasts, 1)
	assert.Equal(t, "open_doc", broadcasts[0].Type)

	data, ok := broadcasts[0].Data.(map[string]string)
	require.True(t, ok, "broadcast data should be map[string]string")
	// The handler may resolve symlinks (e.g. /tmp → /private/tmp on macOS),
	// so compare the basename instead of the full path.
	assert.Equal(t, filepath.Base(path), filepath.Base(data["path"]))
}

func TestOpenDoc_rejectsMissingPath(t *testing.T) {
	api, _ := newDocsAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/docs/open", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	api.handleOpenDoc(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOpenDoc_rejectsDisallowedExt(t *testing.T) {
	api, _ := newDocsAPI(t)
	body, _ := json.Marshal(map[string]string{"path": "/etc/passwd"})
	req := httptest.NewRequest(http.MethodPost, "/docs/open", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.handleOpenDoc(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestOpenDoc_rejectsOutsideHome(t *testing.T) {
	api, fs := newDocsAPI(t)
	body, _ := json.Marshal(map[string]string{"path": "/tmp/workshop-outside.md"})
	req := httptest.NewRequest(http.MethodPost, "/docs/open", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.handleOpenDoc(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, fs.Broadcasts(), "no broadcast on rejected path")
}

// --- handleListMarkdown ---

func TestListMarkdown_findsMdFiles(t *testing.T) {
	api, _ := newDocsAPI(t)
	// Create a tempdir under $HOME with nested .md + non-md files.
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, "workshop-list-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("x"), 0o644))
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "inside.md"), []byte("x"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/docs/list?dir="+url.QueryEscape(dir), nil)
	w := httptest.NewRecorder()
	api.handleListMarkdown(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var files []map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&files))

	names := make(map[string]bool)
	for _, f := range files {
		names[f["name"]] = true
	}
	assert.True(t, names["top.md"], "should include top.md")
	assert.True(t, names[filepath.Join("nested", "inside.md")], "should include nested .md")
	assert.False(t, names["skip.txt"], "should skip non-md files")
}

func TestListMarkdown_skipsHiddenAndNodeModules(t *testing.T) {
	api, _ := newDocsAPI(t)
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, "workshop-list-skip-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.md"), []byte("x"), 0o644))
	nm := filepath.Join(dir, "node_modules")
	require.NoError(t, os.Mkdir(nm, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nm, "leak.md"), []byte("x"), 0o644))
	hidden := filepath.Join(dir, ".git")
	require.NoError(t, os.Mkdir(hidden, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hidden, "leak.md"), []byte("x"), 0o644))
	vendor := filepath.Join(dir, "vendor")
	require.NoError(t, os.Mkdir(vendor, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendor, "leak.md"), []byte("x"), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/docs/list?dir="+url.QueryEscape(dir), nil)
	w := httptest.NewRecorder()
	api.handleListMarkdown(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var files []map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&files))

	for _, f := range files {
		assert.False(t, strings.Contains(f["path"], "node_modules"), "node_modules should be skipped: %s", f["path"])
		assert.False(t, strings.Contains(f["path"], "/.git/"), ".git should be skipped: %s", f["path"])
		assert.False(t, strings.Contains(f["path"], "/vendor/"), "vendor should be skipped: %s", f["path"])
	}
	// Sanity: real.md is still present.
	found := false
	for _, f := range files {
		if f["name"] == "real.md" {
			found = true
		}
	}
	assert.True(t, found, "real.md should be listed")
}

func TestListMarkdown_rejectsOutsideHome(t *testing.T) {
	api, _ := newDocsAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/docs/list?dir=/tmp", nil)
	w := httptest.NewRecorder()
	api.handleListMarkdown(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
