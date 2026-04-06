package v1

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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

	content, err := os.ReadFile(path)
	if err != nil {
		a.jsonError(w, "file not found", http.StatusNotFound)
		return
	}

	a.jsonOK(w, map[string]string{
		"path":    path,
		"name":    filepath.Base(path),
		"content": string(content),
	})
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
