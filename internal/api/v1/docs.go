package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// confineToHome ensures path (after ~ expansion by caller) resolves within the
// user's home directory. It rejects paths outside home *before* touching the
// filesystem to avoid leaking existence of files outside home. If the path
// exists, it additionally resolves symlinks and re-checks to block symlink
// escapes. Returns the resolved path to use for I/O.
func confineToHome(path string) (resolved string, status int, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	homeReal, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", http.StatusBadRequest, err
	}
	// First check: lexical containment against real home. This rejects
	// non-existent paths outside home with 403 rather than leaking via 404.
	if !withinDir(homeReal, absPath) {
		return "", http.StatusForbidden, os.ErrPermission
	}
	// Second check: resolve symlinks and re-verify, to block symlink escapes
	// from within home. If the path doesn't exist, fall through with absPath.
	realPath, evalErr := filepath.EvalSymlinks(absPath)
	if evalErr != nil {
		if os.IsNotExist(evalErr) {
			return absPath, 0, nil
		}
		return "", http.StatusInternalServerError, evalErr
	}
	if !withinDir(homeReal, realPath) {
		return "", http.StatusForbidden, os.ErrPermission
	}
	return realPath, 0, nil
}

func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func (a *API) handleReadFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		a.jsonError(w, "path is required", http.StatusBadRequest)
		return
	}

	// Expand ~ to home dir
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	// Security: only allow reading .md, .txt, .yaml, .yml, .json, .lua files
	ext := strings.ToLower(filepath.Ext(path))
	allowed := map[string]bool{".md": true, ".txt": true, ".yaml": true, ".yml": true, ".json": true, ".lua": true, ".toml": true}
	if !allowed[ext] {
		a.jsonError(w, "file type not allowed", http.StatusForbidden)
		return
	}

	realPath, status, err := confineToHome(path)
	if err != nil {
		switch status {
		case http.StatusForbidden:
			a.jsonError(w, "path not allowed", http.StatusForbidden)
		case http.StatusBadRequest:
			a.jsonError(w, "invalid path", http.StatusBadRequest)
		default:
			a.jsonError(w, "cannot resolve path", http.StatusInternalServerError)
		}
		return
	}

	content, err := os.ReadFile(realPath)
	if err != nil {
		if os.IsNotExist(err) {
			a.jsonError(w, "file not found", http.StatusNotFound)
		} else {
			a.jsonError(w, "cannot read file", http.StatusInternalServerError)
		}
		return
	}

	a.jsonOK(w, map[string]string{
		"path":    realPath,
		"name":    filepath.Base(realPath),
		"content": string(content),
	})
}

func (a *API) handleOpenDoc(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		a.jsonError(w, "path is required", http.StatusBadRequest)
		return
	}

	path := req.Path
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	ext := strings.ToLower(filepath.Ext(path))
	allowed := map[string]bool{".md": true, ".txt": true, ".yaml": true, ".yml": true, ".json": true, ".lua": true, ".toml": true}
	if !allowed[ext] {
		a.jsonError(w, "file type not allowed", http.StatusForbidden)
		return
	}

	realPath, status, err := confineToHome(path)
	if err != nil {
		switch status {
		case http.StatusForbidden:
			a.jsonError(w, "path not allowed", http.StatusForbidden)
		case http.StatusBadRequest:
			a.jsonError(w, "invalid path", http.StatusBadRequest)
		default:
			a.jsonError(w, "cannot resolve path", http.StatusInternalServerError)
		}
		return
	}

	a.status.Broadcast("open_doc", map[string]string{"path": realPath})
	a.jsonOK(w, map[string]string{"path": realPath})
}

func (a *API) handleSearchDocs(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	q := r.URL.Query().Get("q")
	if q == "" {
		a.jsonError(w, "q (search query) is required", http.StatusBadRequest)
		return
	}
	if dir == "" {
		dir = "."
	}
	if strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, dir[2:])
	}

	realDir, status, err := confineToHome(dir)
	if err != nil {
		switch status {
		case http.StatusForbidden:
			a.jsonError(w, "path not allowed", http.StatusForbidden)
		case http.StatusBadRequest:
			a.jsonError(w, "invalid dir", http.StatusBadRequest)
		default:
			a.jsonError(w, "cannot resolve path", http.StatusInternalServerError)
		}
		return
	}
	dir = realDir

	qLower := strings.ToLower(q)
	type searchResult struct {
		Path    string `json:"path"`
		Name    string `json:"name"`
		Line    int    `json:"line"`
		Context string `json:"context"`
	}
	var results []searchResult
	const maxResults = 50

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(results) >= maxResults {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		relPath, _ := filepath.Rel(dir, path)

		// Check filename match
		if strings.Contains(strings.ToLower(relPath), qLower) {
			ctx := relPath
			if len(lines) > 0 {
				ctx = strings.TrimSpace(lines[0])
				if len(ctx) > 120 {
					ctx = ctx[:120] + "..."
				}
			}
			results = append(results, searchResult{Path: path, Name: relPath, Line: 1, Context: ctx})
		}

		// Check content matches
		for i, line := range lines {
			if len(results) >= maxResults {
				break
			}
			if strings.Contains(strings.ToLower(line), qLower) {
				ctx := strings.TrimSpace(line)
				if len(ctx) > 120 {
					ctx = ctx[:120] + "..."
				}
				results = append(results, searchResult{Path: path, Name: relPath, Line: i + 1, Context: ctx})
			}
		}
		return nil
	})

	if results == nil {
		results = []searchResult{}
	}
	a.jsonOK(w, results)
}

func (a *API) handleListMarkdown(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "."
	}

	if strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, dir[2:])
	}

	realDir, status, err := confineToHome(dir)
	if err != nil {
		switch status {
		case http.StatusForbidden:
			a.jsonError(w, "path not allowed", http.StatusForbidden)
		case http.StatusBadRequest:
			a.jsonError(w, "invalid dir", http.StatusBadRequest)
		default:
			a.jsonError(w, "cannot resolve path", http.StatusInternalServerError)
		}
		return
	}
	dir = realDir

	var files []map[string]string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip hidden dirs and node_modules
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(path), ".md") {
			relPath, _ := filepath.Rel(dir, path)
			files = append(files, map[string]string{
				"path": path,
				"name": relPath,
			})
		}
		return nil
	})

	if files == nil {
		files = []map[string]string{}
	}
	a.jsonOK(w, files)
}
