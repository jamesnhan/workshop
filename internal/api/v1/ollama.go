package v1

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jamesnhan/workshop/internal/ollama"
)

func (a *API) handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	if a.ollama == nil {
		a.jsonError(w, "no ollama endpoints configured", http.StatusServiceUnavailable)
		return
	}
	models, err := a.ollama.ListModels(r.Context())
	if err != nil {
		a.logger.Warn("ollama model list partial failure", "err", err)
	}
	if models == nil {
		models = []ollama.Model{}
	}
	a.jsonOK(w, models)
}

func (a *API) handleOllamaHealth(w http.ResponseWriter, r *http.Request) {
	if a.ollama == nil {
		a.jsonOK(w, []ollama.EndpointStatus{})
		return
	}
	statuses := a.ollama.Health(r.Context())
	a.jsonOK(w, statuses)
}

func (a *API) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	if a.ollama == nil {
		a.jsonError(w, "no ollama endpoints configured", http.StatusServiceUnavailable)
		return
	}

	var req ollama.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		a.jsonError(w, "model is required", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		a.jsonError(w, "messages is required", http.StatusBadRequest)
		return
	}

	if req.Stream {
		// Stream via NDJSON
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)
		if !ok {
			a.jsonError(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		err := a.ollama.Chat(r.Context(), req, func(chunk ollama.ChatResponse) {
			line, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		})
		if err != nil {
			// If we've already started writing, we can't send a proper error
			line, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}
		return
	}

	// Non-streaming: collect full response
	var full ollama.ChatResponse
	req.Stream = false
	err := a.ollama.Chat(r.Context(), req, func(chunk ollama.ChatResponse) {
		full = chunk
	})
	if err != nil {
		a.serverErr(w, "ollama chat failed", err)
		return
	}
	a.jsonOK(w, full)
}

func (a *API) handleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	if a.ollama == nil {
		a.jsonError(w, "no ollama endpoints configured", http.StatusServiceUnavailable)
		return
	}

	var req ollama.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		a.jsonError(w, "model is required", http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		a.jsonError(w, "prompt is required", http.StatusBadRequest)
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)
		if !ok {
			a.jsonError(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		err := a.ollama.Generate(r.Context(), req, func(chunk ollama.GenerateResponse) {
			line, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		})
		if err != nil {
			line, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}
		return
	}

	// Non-streaming
	var full ollama.GenerateResponse
	req.Stream = false
	err := a.ollama.Generate(r.Context(), req, func(chunk ollama.GenerateResponse) {
		full = chunk
	})
	if err != nil {
		a.serverErr(w, "ollama generate failed", err)
		return
	}
	a.jsonOK(w, full)
}
