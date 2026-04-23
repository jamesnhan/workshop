package v1

import (
	"encoding/json"
	"net/http"
)

// handleDebugLog accepts arbitrary JSON from the frontend and logs it
// via slog so it flows to Loki/Grafana when OTel is enabled. Useful for
// debugging mobile issues where the browser console isn't accessible.
func (a *API) handleDebugLog(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	msg, _ := payload["msg"].(string)
	if msg == "" {
		msg = "frontend-debug"
	}
	delete(payload, "msg")

	args := make([]any, 0, len(payload)*2)
	for k, v := range payload {
		args = append(args, k, v)
	}
	a.logger.Info(msg, args...)
	w.WriteHeader(http.StatusNoContent)
}
