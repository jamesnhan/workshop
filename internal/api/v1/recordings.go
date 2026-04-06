package v1

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jamesnhan/workshop/internal/db"
)

func (a *API) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	recs, err := a.db.ListRecordings()
	if err != nil {
		a.serverErr(w, "list recordings", err)
		return
	}
	if recs == nil {
		recs = []db.Recording{}
	}
	a.jsonOK(w, recs)
}

func (a *API) handleGetRecording(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	frames, err := a.db.GetRecordingFrames(id)
	if err != nil {
		a.serverErr(w, "get recording frames", err)
		return
	}

	meta, err := a.db.GetRecording(id)
	if err != nil {
		a.jsonError(w, "not found", http.StatusNotFound)
		return
	}

	a.jsonOK(w, map[string]any{
		"recording": meta,
		"frames":    frames,
	})
}

func (a *API) handleStartRecording(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Target string `json:"target"`
		Cols   int    `json:"cols"`
		Rows   int    `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Target == "" {
		a.jsonError(w, "target required", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "Recording"
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	id, err := a.recorder.Start(req.Target, req.Name, req.Cols, req.Rows)
	if err != nil {
		a.serverErr(w, "start recording", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, map[string]any{"id": id})
}

func (a *API) handleStopRecording(w http.ResponseWriter, r *http.Request) {
	// The REST endpoint takes a recording ID, but the recorder is keyed by target.
	// Look up the target from the recording metadata.
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Also accept target in body for direct stop
	var req struct {
		Target string `json:"target"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Target != "" {
		a.recorder.Stop(req.Target)
	} else {
		if rec, err := a.db.GetRecording(id); err == nil && rec.Status == "recording" {
			a.recorder.Stop(rec.Target)
		}
	}
	a.jsonOK(w, map[string]string{"status": "stopped"})
}

func (a *API) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := a.db.DeleteRecording(id); err != nil {
		a.serverErr(w, "delete recording", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

