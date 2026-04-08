package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// UI command REST endpoints. Each tool is exposed as POST /ui/<action>
// with a JSON body specific to the action. Blocking actions (prompt_user,
// confirm) wait on the UIHub for the frontend to POST a response to
// /ui/response/{id}.

const uiResponseTimeout = 5 * time.Minute

func (a *API) handleUIAction(action string, blocking bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			payload = map[string]any{}
		}
		if !blocking {
			a.ui.Send(action, payload)
			a.jsonOK(w, map[string]string{"status": "sent"})
			return
		}
		resp, err := a.ui.SendAndWait(action, payload, uiResponseTimeout)
		if err != nil {
			a.jsonError(w, "ui command timed out", http.StatusGatewayTimeout)
			return
		}
		if resp.Cancelled {
			a.jsonError(w, "user cancelled", http.StatusNoContent)
			return
		}
		a.jsonOK(w, resp)
	}
}

// handleUIResponse receives the frontend's reply for a blocking command
// and forwards it to the waiting goroutine via the UIHub.
func (a *API) handleUIResponse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		a.jsonError(w, "id is required", http.StatusBadRequest)
		return
	}
	var resp UIResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
		// allow empty body
	}
	if !a.ui.Resolve(id, resp) {
		a.jsonError(w, "unknown or expired ui command id", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
