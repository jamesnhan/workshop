package v1

import (
	"net/http"
	"strconv"
)

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		a.jsonError(w, "q parameter is required", http.StatusBadRequest)
		return
	}
	target := r.URL.Query().Get("target")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	results := a.searcher.SearchJSON(query, target, limit)
	if results == nil {
		results = []map[string]any{}
	}
	a.jsonOK(w, results)
}

// handleListLines returns all buffered lines for client-side fzf search.
func (a *API) handleListLines(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	results := a.searcher.ListAll(target)
	if results == nil {
		results = []map[string]any{}
	}
	a.jsonOK(w, results)
}

func (a *API) handleSearchContext(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		a.jsonError(w, "target is required", http.StatusBadRequest)
		return
	}
	line := 1
	if l := r.URL.Query().Get("line"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			line = n
		}
	}
	ctx := 5
	if c := r.URL.Query().Get("context"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			ctx = n
		}
	}
	lines := a.searcher.GetContext(target, line, ctx)
	if lines == nil {
		lines = []string{}
	}
	a.jsonOK(w, map[string]any{
		"target":    target,
		"line":      line,
		"context":   ctx,
		"lines":     lines,
		"startLine": max(1, line-ctx),
	})
}
