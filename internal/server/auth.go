package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// authMiddleware rejects requests that don't carry a valid API key when one is
// configured via the WORKSHOP_API_KEY environment variable.  If apiKey is empty,
// all requests pass through (backward-compat for local dev).
//
// Exempt paths:
//   - GET /api/v1/health  (K8s liveness/readiness probes)
//   - Static frontend files (anything that is NOT /api/* and NOT /ws)
func authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Exempt health endpoint and static frontend assets.
		if r.URL.Path == "/api/v1/health" || (!strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/ws") {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization: Bearer <key> header first, then ?token= query
		// param (needed for WebSocket connections where custom headers are
		// impractical).
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || token == r.Header.Get("Authorization") {
			// TrimPrefix returned the original string — no "Bearer " prefix.
			token = ""
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token != apiKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}

		next.ServeHTTP(w, r)
	})
}
