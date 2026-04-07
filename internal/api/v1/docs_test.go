package v1

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func readFileReq(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	a := &API{}
	req := httptest.NewRequest("GET", "/docs/read?path="+url.QueryEscape(path), nil)
	w := httptest.NewRecorder()
	a.handleReadFile(w, req)
	return w
}

func TestHandleReadFile_RejectsOutsideHome(t *testing.T) {
	// Nonexistent file outside home — must be 403, not 404 (no existence leak).
	cases := []string{
		"/etc/hosts.json",           // nonexistent, outside home
		"/etc/passwd.txt",           // nonexistent, outside home
		"/tmp/does-not-exist.md",    // nonexistent, outside home
	}
	for _, p := range cases {
		w := readFileReq(t, p)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: want 403, got %d body=%s", p, w.Code, w.Body.String())
		}
	}
}

func TestHandleReadFile_RejectsTraversal(t *testing.T) {
	home, _ := os.UserHomeDir()
	// Traversal out of home via ..
	traversal := filepath.Join(home, "..", "..", "etc", "hosts.json")
	w := readFileReq(t, traversal)
	if w.Code != http.StatusForbidden {
		t.Errorf("traversal: want 403, got %d", w.Code)
	}
}

func TestHandleReadFile_RejectsDisallowedExt(t *testing.T) {
	w := readFileReq(t, "/etc/passwd")
	if w.Code != http.StatusForbidden {
		t.Errorf("want 403 for disallowed ext, got %d", w.Code)
	}
}

func TestHandleReadFile_AllowsInsideHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// Create a temp .md file inside home.
	f, err := os.CreateTemp(home, "workshop-docs-test-*.md")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	w := readFileReq(t, f.Name())
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleReadFile_RejectsSymlinkEscape(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// Create a symlink inside home pointing to /etc/hosts (outside home).
	link := filepath.Join(home, "workshop-docs-test-escape.json")
	_ = os.Remove(link)
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	defer os.Remove(link)

	w := readFileReq(t, link)
	if w.Code != http.StatusForbidden {
		t.Errorf("symlink escape: want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleReadFile_MissingPath(t *testing.T) {
	w := readFileReq(t, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}
