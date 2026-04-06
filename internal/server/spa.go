package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

func spaHandler(fsys embed.FS) http.Handler {
	distFS, _ := fs.Sub(fsys, "frontend/dist")
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(distFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for client-side routing
		index, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}
