package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// kanbanListHandler
// ---------------------------------------------------------------------------

func TestKanbanListHandler_paginationParams(t *testing.T) {
	mux := withFakeAPI(t)
	var gotQuery map[string]string
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = map[string]string{
			"project": r.URL.Query().Get("project"),
			"limit":   r.URL.Query().Get("limit"),
			"offset":  r.URL.Query().Get("offset"),
		}
		resp := `{"cards":[{"id":1,"title":"A","column":"backlog"}],"total":3,"limit":2,"offset":0}`
		w.Write([]byte(resp))
	})

	res := callTool(t, kanbanListHandler(), map[string]any{
		"project": "workshop",
		"limit":   float64(2),
		"offset":  float64(0),
	})
	assert.False(t, isError(res))
	text := resultText(res)
	assert.Contains(t, text, "Showing 1–1 of 3 cards")
	assert.Contains(t, text, "(next: offset=1)")
	assert.Equal(t, "workshop", gotQuery["project"])
	assert.Equal(t, "2", gotQuery["limit"])
}

func TestKanbanListHandler_noCards(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"cards":[],"total":0,"limit":50,"offset":0}`))
	})
	res := callTool(t, kanbanListHandler(), map[string]any{})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "No cards found")
}

func TestKanbanListHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "db locked", http.StatusInternalServerError)
	})
	res := callTool(t, kanbanListHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "db locked")
}

func TestKanbanListHandler_showsProjectAndDescription(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		resp := `{"cards":[{"id":5,"title":"Fix bug","column":"in_progress","project":"workshop","description":"important fix"}],"total":1,"limit":50,"offset":0}`
		w.Write([]byte(resp))
	})
	res := callTool(t, kanbanListHandler(), map[string]any{})
	text := resultText(res)
	assert.Contains(t, text, "(workshop)")
	assert.Contains(t, text, "important fix")
	assert.Contains(t, text, "Fix bug")
}

func TestKanbanListHandler_includeArchived(t *testing.T) {
	mux := withFakeAPI(t)
	var gotArchived string
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		gotArchived = r.URL.Query().Get("include_archived")
		w.Write([]byte(`{"cards":[],"total":0,"limit":50,"offset":0}`))
	})
	callTool(t, kanbanListHandler(), map[string]any{"include_archived": true})
	assert.Equal(t, "true", gotArchived)
}

func TestKanbanListHandler_defaultLimitIs50(t *testing.T) {
	mux := withFakeAPI(t)
	var gotLimit string
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		w.Write([]byte(`{"cards":[],"total":0,"limit":50,"offset":0}`))
	})
	callTool(t, kanbanListHandler(), map[string]any{})
	assert.Equal(t, "50", gotLimit)
}

// ---------------------------------------------------------------------------
// kanbanCreateHandler
// ---------------------------------------------------------------------------

func TestKanbanCreateHandler_allFields(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":42,"title":"new card"}`))
	})
	res := callTool(t, kanbanCreateHandler(), map[string]any{
		"title":       "new card",
		"description": "do the thing",
		"column":      "in_progress",
		"project":     "workshop",
		"card_type":   "feature",
		"priority":    "P0",
		"labels":      "urgent",
		"parent_id":   float64(10),
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Created card #42")
	assert.Equal(t, "new card", received["title"])
	assert.Equal(t, "do the thing", received["description"])
	assert.Equal(t, "in_progress", received["column"])
	assert.Equal(t, "workshop", received["project"])
	assert.Equal(t, "feature", received["cardType"])
	assert.Equal(t, "P0", received["priority"])
	assert.Equal(t, "urgent", received["labels"])
	assert.Equal(t, float64(10), received["parentId"])
}

func TestKanbanCreateHandler_defaultColumn(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1,"title":"t"}`))
	})
	callTool(t, kanbanCreateHandler(), map[string]any{"title": "t"})
	assert.Equal(t, "backlog", received["column"])
}

func TestKanbanCreateHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "validation failed", http.StatusBadRequest)
	})
	res := callTool(t, kanbanCreateHandler(), map[string]any{"title": "t"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "validation failed")
}

// ---------------------------------------------------------------------------
// kanbanEditHandler
// ---------------------------------------------------------------------------

func TestKanbanEditHandler_requiresId(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, kanbanEditHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "id is required")
}

func TestKanbanEditHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var putBody map[string]any
	var putMethod string

	// GET returns a list of cards (kanbanEditHandler fetches all, then finds by id)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":5,"title":"old","column":"backlog","project":"workshop"}]`))
	})
	// PUT updates the card
	mux.HandleFunc("/api/v1/cards/5", func(w http.ResponseWriter, r *http.Request) {
		putMethod = r.Method
		json.NewDecoder(r.Body).Decode(&putBody)
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, kanbanEditHandler(), map[string]any{
		"id":    float64(5),
		"title": "updated",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Updated card #5")
	assert.Equal(t, "PUT", putMethod)
	assert.Equal(t, "updated", putBody["title"])
}

func TestKanbanEditHandler_cardNotFound(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	res := callTool(t, kanbanEditHandler(), map[string]any{"id": float64(99)})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "Card #99 not found")
}

func TestKanbanEditHandler_putError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`[{"id":5,"title":"old","column":"backlog"}]`))
			return
		}
	})
	mux.HandleFunc("/api/v1/cards/5", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	})
	res := callTool(t, kanbanEditHandler(), map[string]any{
		"id":    float64(5),
		"title": "new",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "conflict")
}

// ---------------------------------------------------------------------------
// kanbanMoveHandler
// ---------------------------------------------------------------------------

func TestKanbanMoveHandler_requiresIdAndColumn(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, kanbanMoveHandler(), map[string]any{"id": float64(1)})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "id and column are required")

	res = callTool(t, kanbanMoveHandler(), map[string]any{"column": "done"})
	assert.True(t, isError(res))
}

func TestKanbanMoveHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	var gotPath string
	mux.HandleFunc("/api/v1/cards/7/move", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})
	res := callTool(t, kanbanMoveHandler(), map[string]any{
		"id":     float64(7),
		"column": "done",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Moved card #7 to done")
	assert.Equal(t, "/api/v1/cards/7/move", gotPath)
	assert.Equal(t, "done", received["column"])
}

func TestKanbanMoveHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards/1/move", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "transition not allowed", http.StatusBadRequest)
	})
	res := callTool(t, kanbanMoveHandler(), map[string]any{
		"id":     float64(1),
		"column": "done",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "transition not allowed")
}

// ---------------------------------------------------------------------------
// kanbanAddNoteHandler
// ---------------------------------------------------------------------------

func TestKanbanAddNoteHandler_requiresIdAndText(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, kanbanAddNoteHandler(), map[string]any{"id": float64(1)})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "id and text are required")

	res = callTool(t, kanbanAddNoteHandler(), map[string]any{"text": "hi"})
	assert.True(t, isError(res))
}

func TestKanbanAddNoteHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]string
	mux.HandleFunc("/api/v1/cards/3/notes", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
	})
	res := callTool(t, kanbanAddNoteHandler(), map[string]any{
		"id":   float64(3),
		"text": "investigation complete",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Note added to card #3")
	assert.Equal(t, "investigation complete", received["text"])
}

func TestKanbanAddNoteHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/cards/3/notes", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "card not found", http.StatusNotFound)
	})
	res := callTool(t, kanbanAddNoteHandler(), map[string]any{
		"id":   float64(3),
		"text": "note",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "card not found")
}

// ---------------------------------------------------------------------------
// kanbanDeleteHandler
// ---------------------------------------------------------------------------

func TestKanbanDeleteHandler_requiresId(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, kanbanDeleteHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "id is required")
}

func TestKanbanDeleteHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var gotMethod string
	mux.HandleFunc("/api/v1/cards/10", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})
	res := callTool(t, kanbanDeleteHandler(), map[string]any{"id": float64(10)})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Deleted card #10")
	assert.Equal(t, "DELETE", gotMethod)
}

// ---------------------------------------------------------------------------
// reportActivityHandler
// ---------------------------------------------------------------------------

func TestReportActivityHandler_requiresActionAndSummary(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, reportActivityHandler(), map[string]any{"action": "test"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "action and summary are required")

	res = callTool(t, reportActivityHandler(), map[string]any{"summary": "did stuff"})
	assert.True(t, isError(res))
}

func TestReportActivityHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/activity", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
	})

	// Set mcpPaneTarget so it gets included in the payload
	old := mcpPaneTarget
	mcpPaneTarget = "test:1.0"
	t.Cleanup(func() { mcpPaneTarget = old })

	res := callTool(t, reportActivityHandler(), map[string]any{
		"action":    "implement",
		"summary":   "added feature X",
		"project":   "workshop",
		"metadata":  `{"files":3}`,
		"parent_id": float64(42),
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Activity recorded")
	assert.Equal(t, "implement", received["actionType"])
	assert.Equal(t, "added feature X", received["summary"])
	assert.Equal(t, "workshop", received["project"])
	assert.Equal(t, `{"files":3}`, received["metadata"])
	assert.Equal(t, "test:1.0", received["paneTarget"])
	assert.Equal(t, float64(42), received["parentId"])
}

func TestReportActivityHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/activity", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	res := callTool(t, reportActivityHandler(), map[string]any{
		"action":  "test",
		"summary": "did stuff",
	})
	assert.True(t, isError(res))
}


// ---------------------------------------------------------------------------
// ollamaModelsHandler
// ---------------------------------------------------------------------------

func TestOllamaModelsHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/models", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"name":"llama3:8b","endpoint":"4090","size":4500000000}]`))
	})
	res := callTool(t, ollamaModelsHandler(), nil)
	assert.False(t, isError(res))
	text := resultText(res)
	assert.Contains(t, text, "[4090]")
	assert.Contains(t, text, "llama3:8b")
	assert.Contains(t, text, "4.5 GB")
}

func TestOllamaModelsHandler_noModels(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/models", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	res := callTool(t, ollamaModelsHandler(), nil)
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "No models available")
}

func TestOllamaModelsHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/models", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ollama down", http.StatusBadGateway)
	})
	res := callTool(t, ollamaModelsHandler(), nil)
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "ollama down")
}

// ---------------------------------------------------------------------------
// ollamaChatHandler
// ---------------------------------------------------------------------------

func TestOllamaChatHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/ollama/chat", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"message":{"content":"Hello there"},"eval_count":50,"eval_duration":1000000000}`))
	})
	// Swallow usage POST
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, ollamaChatHandler(), map[string]any{
		"model":  "llama3:8b",
		"prompt": "say hi",
	})
	assert.False(t, isError(res))
	text := resultText(res)
	assert.Contains(t, text, "Hello there")
	assert.Contains(t, text, "50 tokens")
	assert.Contains(t, text, "50.0 tok/s")
	assert.Equal(t, "llama3:8b", received["model"])
}

func TestOllamaChatHandler_thinkingFallback(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/chat", func(w http.ResponseWriter, r *http.Request) {
		// content is empty, thinking is populated — handler should use thinking
		w.Write([]byte(`{"message":{"content":"","thinking":"I thought about it carefully"}}`))
	})
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, ollamaChatHandler(), map[string]any{
		"model":  "qwq:32b",
		"prompt": "think about this",
		"think":  true,
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "I thought about it carefully")
}

func TestOllamaChatHandler_emptyResponse(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":""}}`))
	})
	res := callTool(t, ollamaChatHandler(), map[string]any{
		"model":  "llama3:8b",
		"prompt": "say nothing",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "(empty response)")
}

func TestOllamaChatHandler_withSystemAndTemperature(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/ollama/chat", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"message":{"content":"ok"}}`))
	})
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	callTool(t, ollamaChatHandler(), map[string]any{
		"model":       "llama3:8b",
		"prompt":      "hello",
		"system":      "you are helpful",
		"temperature": float64(0.7),
		"max_tokens":  float64(1000),
	})

	// With system prompt, messages should include system + user
	msgs, ok := received["messages"].([]any)
	require.True(t, ok)
	assert.Len(t, msgs, 2)
	assert.Equal(t, 0.7, received["temperature"])
	assert.Equal(t, float64(1000), received["max_tokens"])
}

func TestOllamaChatHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/chat", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	})
	res := callTool(t, ollamaChatHandler(), map[string]any{
		"model":  "nonexistent",
		"prompt": "hi",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "model not found")
}

// ---------------------------------------------------------------------------
// ollamaGenerateHandler
// ---------------------------------------------------------------------------

func TestOllamaGenerateHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/ollama/generate", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"response":"generated text","eval_count":100,"eval_duration":2000000000}`))
	})
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, ollamaGenerateHandler(), map[string]any{
		"model":  "llama3:8b",
		"prompt": "write a story",
	})
	assert.False(t, isError(res))
	text := resultText(res)
	assert.Contains(t, text, "generated text")
	assert.Contains(t, text, "100 tokens")
	assert.Contains(t, text, "50.0 tok/s")
}

func TestOllamaGenerateHandler_emptyResponse(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"response":""}`))
	})
	res := callTool(t, ollamaGenerateHandler(), map[string]any{
		"model":  "llama3:8b",
		"prompt": "nothing",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "(empty response)")
}

func TestOllamaGenerateHandler_withSystemPrompt(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/ollama/generate", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"response":"ok"}`))
	})
	mux.HandleFunc("/api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	callTool(t, ollamaGenerateHandler(), map[string]any{
		"model":  "llama3:8b",
		"prompt": "hello",
		"system": "be concise",
	})
	assert.Equal(t, "be concise", received["system"])
}

func TestOllamaGenerateHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ollama/generate", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	res := callTool(t, ollamaGenerateHandler(), map[string]any{
		"model":  "x",
		"prompt": "y",
	})
	assert.True(t, isError(res))
}

// ---------------------------------------------------------------------------
// clearPaneStatusHandler — default target fallback
// ---------------------------------------------------------------------------

func TestClearPaneStatusHandler_defaultsToMcpPaneTarget(t *testing.T) {
	mux := withFakeAPI(t)
	old := mcpPaneTarget
	mcpPaneTarget = "auto:1.0"
	t.Cleanup(func() { mcpPaneTarget = old })

	var received map[string]string
	mux.HandleFunc("/api/v1/panes/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})

	// No target provided — should fall back to mcpPaneTarget
	res := callTool(t, clearPaneStatusHandler(), map[string]any{})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Status cleared for auto:1.0")
	assert.Equal(t, "auto:1.0", received["target"])
}

// ---------------------------------------------------------------------------
// setPaneStatusHandler — default target fallback
// ---------------------------------------------------------------------------

func TestSetPaneStatusHandler_defaultsToMcpPaneTarget(t *testing.T) {
	mux := withFakeAPI(t)
	old := mcpPaneTarget
	mcpPaneTarget = "auto:2.0"
	t.Cleanup(func() { mcpPaneTarget = old })

	var received map[string]string
	mux.HandleFunc("/api/v1/panes/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})

	res := callTool(t, setPaneStatusHandler(), map[string]any{
		"status":  "green",
		"message": "done",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "auto:2.0")
	assert.Equal(t, "auto:2.0", received["target"])
}

// ---------------------------------------------------------------------------
// requestApprovalHandler
// ---------------------------------------------------------------------------

func TestRequestApprovalHandler_requiresActionAndDetails(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, requestApprovalHandler(), map[string]any{"action": "deploy"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "action and details are required")
}

func TestRequestApprovalHandler_approved(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"approved"}`))
	})
	res := callTool(t, requestApprovalHandler(), map[string]any{
		"action":  "deploy",
		"details": "push to prod",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Approved")
	assert.Contains(t, resultText(res), "deploy")
}

func TestRequestApprovalHandler_denied(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"denied","reason":"not ready"}`))
	})
	res := callTool(t, requestApprovalHandler(), map[string]any{
		"action":  "deploy",
		"details": "push to prod",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "Denied")
	assert.Contains(t, resultText(res), "not ready")
}

// ---------------------------------------------------------------------------
// searchOutputHandler
// ---------------------------------------------------------------------------

func TestSearchOutputHandler_requiresQuery(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, searchOutputHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "query is required")
}

func TestSearchOutputHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var gotQuery string
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Write([]byte(`[{"target":"main:1.0","line":42,"content":"error: connection refused"}]`))
	})
	res := callTool(t, searchOutputHandler(), map[string]any{
		"query": "error",
	})
	assert.False(t, isError(res))
	text := resultText(res)
	assert.Contains(t, text, "main:1.0")
	assert.Contains(t, text, "connection refused")
	assert.Equal(t, "error", gotQuery)
}

func TestSearchOutputHandler_noResults(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	res := callTool(t, searchOutputHandler(), map[string]any{"query": "xyz"})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "No results found")
}

func TestSearchOutputHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "search unavailable", http.StatusServiceUnavailable)
	})
	res := callTool(t, searchOutputHandler(), map[string]any{"query": "test"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "search unavailable")
}

// ---------------------------------------------------------------------------
// channelUnsubscribeHandler
// ---------------------------------------------------------------------------

func TestChannelUnsubscribeHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/channels/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.Write([]byte(`{"ok":true}`))
	})
	res := callTool(t, channelUnsubscribeHandler(), map[string]any{
		"channel": "room",
		"target":  "a:1.1",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "unsubscribed")
	assert.Equal(t, "room", received["channel"])
	assert.Equal(t, "a:1.1", received["target"])
}

// ---------------------------------------------------------------------------
// channelListHandler
// ---------------------------------------------------------------------------

func TestChannelListHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var gotProject string
	mux.HandleFunc("/api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		gotProject = r.URL.Query().Get("project")
		w.Write([]byte(`[{"channel":"room","subscribers":2,"messages":5}]`))
	})
	res := callTool(t, channelListHandler(), map[string]any{"project": "workshop"})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "room")
	assert.Equal(t, "workshop", gotProject)
}

func TestChannelListHandler_noProject(t *testing.T) {
	mux := withFakeAPI(t)
	var gotURL string
	mux.HandleFunc("/api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Write([]byte(`[]`))
	})
	callTool(t, channelListHandler(), map[string]any{})
	// No project query param should be present
	assert.Equal(t, "/api/v1/channels", gotURL)
}

// ---------------------------------------------------------------------------
// channelMessagesHandler
// ---------------------------------------------------------------------------

func TestChannelMessagesHandler_requiresChannel(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, channelMessagesHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "channel is required")
}

func TestChannelMessagesHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/channels/room/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"from":"alice","body":"hello"}]`))
	})
	res := callTool(t, channelMessagesHandler(), map[string]any{
		"channel": "room",
		"limit":   float64(10),
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "alice")
}

// ---------------------------------------------------------------------------
// openDocHandler
// ---------------------------------------------------------------------------

func TestOpenDocHandler_requiresPath(t *testing.T) {
	withFakeAPI(t)
	res := callTool(t, openDocHandler(), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "path is required")
}

func TestOpenDocHandler_happyPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]string
	mux.HandleFunc("/api/v1/docs/open", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})
	res := callTool(t, openDocHandler(), map[string]any{"path": "docs/specs/README.md"})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Opened docs/specs/README.md")
	assert.Equal(t, "docs/specs/README.md", received["path"])
}

func TestOpenDocHandler_apiError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/docs/open", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "path outside home", http.StatusForbidden)
	})
	res := callTool(t, openDocHandler(), map[string]any{"path": "/etc/shadow"})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "403")
}

// ---------------------------------------------------------------------------
// uiActionHandler
// ---------------------------------------------------------------------------

func TestUIActionHandler_nonBlocking(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/ui/show_toast", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	})
	h := uiActionHandler("show_toast", false, []string{"message", "kind"})
	res := callTool(t, h, map[string]any{
		"message": "hello",
		"kind":    "success",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "show_toast sent")
	assert.Equal(t, "hello", received["message"])
}

func TestUIActionHandler_blockingApproved(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ui/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":"true","cancelled":false}`))
	})
	h := uiActionHandler("confirm", true, []string{"title", "message"})
	res := callTool(t, h, map[string]any{
		"title":   "Delete?",
		"message": "Are you sure?",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "true", resultText(res))
}

func TestUIActionHandler_blockingCancelled(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ui/prompt_user", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":"","cancelled":true}`))
	})
	h := uiActionHandler("prompt_user", true, []string{"title", "message"})
	res := callTool(t, h, map[string]any{
		"title":   "Name?",
		"message": "Enter name",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "(cancelled by user)")
}

func TestUIActionHandler_blocking204(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ui/confirm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := uiActionHandler("confirm", true, []string{"title", "message"})
	res := callTool(t, h, map[string]any{
		"title":   "x",
		"message": "y",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "(cancelled by user)")
}

func TestUIActionHandler_blockingError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ui/confirm", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	})
	h := uiActionHandler("confirm", true, []string{"title", "message"})
	res := callTool(t, h, map[string]any{
		"title":   "x",
		"message": "y",
	})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "timeout")
}

func TestUIActionHandler_nonBlockingError(t *testing.T) {
	mux := withFakeAPI(t)
	mux.HandleFunc("/api/v1/ui/switch_view", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid view", http.StatusBadRequest)
	})
	h := uiActionHandler("switch_view", false, []string{"view"})
	res := callTool(t, h, map[string]any{"view": "nonexistent"})
	assert.True(t, isError(res))
}

// ---------------------------------------------------------------------------
// runConfigHandler
// ---------------------------------------------------------------------------

func TestRunConfigHandler_postsPath(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]string
	mux.HandleFunc("/api/v1/config/load", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &received)
		w.Write([]byte(`{"result":"ok"}`))
	})
	res := callTool(t, runConfigHandler(), map[string]any{
		"path": "~/.config/workshop/init.lua",
	})
	assert.False(t, isError(res))
	assert.Equal(t, "~/.config/workshop/init.lua", received["path"])
}

// ---------------------------------------------------------------------------
// createSessionHandler
// ---------------------------------------------------------------------------

func TestCreateSessionHandler_requiresName(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, createSessionHandler(b), map[string]any{})
	assert.True(t, isError(res))
	assert.Contains(t, resultText(res), "name is required")
}

func TestCreateSessionHandler_happyPathViaAPI(t *testing.T) {
	mux := withFakeAPI(t)
	var received map[string]any
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusCreated)
	})
	b := &fakeBridge{}
	res := callTool(t, createSessionHandler(b), map[string]any{
		"name":      "dev",
		"directory": "/tmp",
	})
	assert.False(t, isError(res))
	assert.Contains(t, resultText(res), "Session 'dev' created")
	assert.Equal(t, "dev", received["name"])
	assert.Equal(t, "/tmp", received["startDir"])
	// Bridge should NOT be called when API succeeds
	assert.Empty(t, b.createSessionCalls)
}

func TestCreateSessionHandler_fallbackToBridge(t *testing.T) {
	// No fake API — HTTP call will fail, handler should fall back to bridge
	t.Setenv("WORKSHOP_API_URL", "http://127.0.0.1:1") // unreachable
	b := &fakeBridge{}
	res := callTool(t, createSessionHandler(b), map[string]any{
		"name":      "dev",
		"directory": "/tmp",
	})
	assert.False(t, isError(res))
	require.Len(t, b.createSessionCalls, 1)
	assert.Equal(t, "dev", b.createSessionCalls[0].A)
}

// ---------------------------------------------------------------------------
// createWindowHandler
// ---------------------------------------------------------------------------

func TestCreateWindowHandler_happyPath(t *testing.T) {
	b := &fakeBridge{}
	res := callTool(t, createWindowHandler(b), map[string]any{
		"session": "dev",
		"name":    "build",
	})
	assert.False(t, isError(res))
	require.Len(t, b.createWindowCalls, 1)
	assert.Equal(t, "dev", b.createWindowCalls[0].A)
}

func TestSanityCheck(t *testing.T) {
	t.Log("sanity check")
}
