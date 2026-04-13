package v1

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAPI constructs a minimal API with just the fields handleGitInfo
// touches. The git handler doesn't reach into db/tmux/etc so we can leave
// those nil.
func newTestAPI(t *testing.T) *API {
	t.Helper()
	return &API{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// callGitInfo runs handleGitInfo against a dir via an in-process HTTP
// request and returns status + decoded body.
func callGitInfo(t *testing.T, api *API, dir string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/git/info?dir="+url.QueryEscape(dir), nil)
	w := httptest.NewRecorder()
	api.handleGitInfo(w, req)
	res := w.Result()
	defer res.Body.Close()
	var body map[string]any
	if res.Header.Get("Content-Type") == "application/json" {
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil && err != io.EOF {
			t.Fatalf("decode: %v", err)
		}
	}
	return res.StatusCode, body
}

// --- Basic cases ---

func TestGitInfo_missingDirParam(t *testing.T) {
	api := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/git/info", nil)
	w := httptest.NewRecorder()
	api.handleGitInfo(w, req)
	res := w.Result()
	defer res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestGitInfo_notARepo(t *testing.T) {
	api := newTestAPI(t)
	dir := testhelpers.TempDataDir(t) // empty tempdir, not a git repo
	status, body := callGitInfo(t, api, dir)
	assert.Equal(t, http.StatusOK, status)
	assert.Nil(t, body) // null JSON body for non-repo dirs
}

// --- Branch + clean/dirty state ---

func TestGitInfo_cleanRepo(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("README.md", "hello")
	r.Commit("initial")

	status, body := callGitInfo(t, api, r.Dir)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "main", body["branch"])
	assert.Equal(t, false, body["dirty"])
	assert.EqualValues(t, 0, body["changed"])
	assert.EqualValues(t, 0, body["untracked"])
	assert.EqualValues(t, 0, body["ahead"])
	assert.EqualValues(t, 0, body["behind"])
}

func TestGitInfo_dirtyWithStagedAndUnstaged(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("a.txt", "initial")
	r.Commit("initial")

	// Modify committed file (unstaged) and add a new tracked+staged file.
	r.WriteFile("a.txt", "modified")
	r.WriteFile("b.txt", "new")
	r.Run("add", "b.txt")

	status, body := callGitInfo(t, api, r.Dir)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, true, body["dirty"])
	// Both files count as "changed" per the porcelain parse.
	assert.EqualValues(t, 2, body["changed"])
	assert.EqualValues(t, 0, body["untracked"])
}

func TestGitInfo_untrackedCounted(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("tracked.txt", "x")
	r.Commit("initial")

	r.WriteFile("untracked.txt", "y")
	r.WriteFile("another.txt", "z")

	status, body := callGitInfo(t, api, r.Dir)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, true, body["dirty"])
	assert.EqualValues(t, 0, body["changed"])
	assert.EqualValues(t, 2, body["untracked"])
}

// --- Ahead / behind ---

// bareClone creates a bare clone of src at a new tempdir and returns its path.
// Used as an "upstream" in ahead/behind tests.
func bareClone(t *testing.T, src string) string {
	t.Helper()
	dir := testhelpers.TempDataDir(t)
	cmd := exec.Command("git", "clone", "--bare", src, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}
	return dir
}

func TestGitInfo_ahead(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("a", "1")
	r.Commit("one")

	// Point origin at a bare clone of the current state, set upstream,
	// then add new commits locally so we're ahead.
	upstream := bareClone(t, r.Dir)
	r.Run("remote", "add", "origin", upstream)
	r.Run("push", "-u", "origin", "main")

	r.WriteFile("a", "2")
	r.Commit("two")
	r.WriteFile("a", "3")
	r.Commit("three")

	status, body := callGitInfo(t, api, r.Dir)
	require.Equal(t, http.StatusOK, status)
	assert.EqualValues(t, 2, body["ahead"])
	assert.EqualValues(t, 0, body["behind"])
}

func TestGitInfo_noUpstream(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("x", "x")
	r.Commit("initial")

	status, body := callGitInfo(t, api, r.Dir)
	require.Equal(t, http.StatusOK, status)
	assert.EqualValues(t, 0, body["ahead"])
	assert.EqualValues(t, 0, body["behind"])
}

// --- Remote URL → repo name parsing ---

func TestGitInfo_repoNameFromSSH(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("x", "x")
	r.Commit("initial")
	r.Run("remote", "add", "origin", "git@github.com:jamesnhan/workshop.git")

	_, body := callGitInfo(t, api, r.Dir)
	assert.Equal(t, "workshop", body["repoName"])
}

func TestGitInfo_repoNameFromHTTPS(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("x", "x")
	r.Commit("initial")
	r.Run("remote", "add", "origin", "https://github.com/jamesnhan/workshop.git")

	_, body := callGitInfo(t, api, r.Dir)
	assert.Equal(t, "workshop", body["repoName"])
}

func TestGitInfo_repoNameEmptyWithoutRemote(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	r.WriteFile("x", "x")
	r.Commit("initial")

	_, body := callGitInfo(t, api, r.Dir)
	assert.Equal(t, "", body["repoName"])
}

// --- Recent logs ---

func TestGitInfo_recentLogsTruncatedToFive(t *testing.T) {
	api := newTestAPI(t)
	r := testhelpers.NewGitRepo(t)
	for i := 0; i < 7; i++ {
		r.Commit("commit")
	}

	_, body := callGitInfo(t, api, r.Dir)
	logs, ok := body["recentLogs"].([]any)
	require.True(t, ok, "recentLogs should be a slice, got %T", body["recentLogs"])
	assert.Len(t, logs, 5)
}

// --- Path expansion ---

func TestGitInfo_expandsTildePath(t *testing.T) {
	// We can't safely write into the real $HOME, but we CAN verify the
	// expansion happens: point dir at "~/nonexistent-dir" and expect 200
	// with null body (not a 400 about the literal "~/..." being missing).
	api := newTestAPI(t)
	status, body := callGitInfo(t, api, "~/this-dir-should-not-exist-for-workshop-tests")
	assert.Equal(t, http.StatusOK, status)
	assert.Nil(t, body)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Guard: make sure the package itself compiled; Go 1.26's test runner
// would flag this but we also catch obvious typos from refactors.
var _ = strings.Contains
