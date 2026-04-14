package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Endpoint represents a single Ollama server.
type Endpoint struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Default bool   `json:"default,omitempty"`
}

// Model represents an available model on an endpoint.
type Model struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Endpoint   string `json:"endpoint"`    // which endpoint hosts this model
	ModifiedAt string `json:"modifiedAt"`
}

// ChatMessage is a single message in a chat conversation.
type ChatMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

// ChatRequest is the input for a chat completion.
type ChatRequest struct {
	Model       string            `json:"model"`
	Messages    []ChatMessage     `json:"messages"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Think       *bool             `json:"think,omitempty"`
	Stream      bool              `json:"stream"`
	System      string            `json:"system,omitempty"`
	Options     map[string]any    `json:"options,omitempty"`
}

// ChatResponse is a single chunk from a streaming chat response.
type ChatResponse struct {
	Model     string      `json:"model"`
	Message   ChatMessage `json:"message"`
	Done      bool        `json:"done"`
	DoneReason string    `json:"done_reason,omitempty"`
	// Token stats (only on final chunk)
	TotalDuration   int64 `json:"total_duration,omitempty"`
	PromptEvalCount int   `json:"prompt_eval_count,omitempty"`
	EvalCount       int   `json:"eval_count,omitempty"`
	EvalDuration    int64 `json:"eval_duration,omitempty"`
}

// GenerateRequest is the input for a single-shot generation.
type GenerateRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	System      string   `json:"system,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"num_predict,omitempty"`
	Think       *bool    `json:"think,omitempty"`
	Stream      bool     `json:"stream"`
}

// GenerateResponse is a single chunk from a streaming generate response.
type GenerateResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
	// Token stats (only on final chunk)
	TotalDuration   int64 `json:"total_duration,omitempty"`
	PromptEvalCount int   `json:"prompt_eval_count,omitempty"`
	EvalCount       int   `json:"eval_count,omitempty"`
	EvalDuration    int64 `json:"eval_duration,omitempty"`
}

// Client manages connections to multiple Ollama endpoints.
type Client struct {
	endpoints []Endpoint
	http      *http.Client
	mu        sync.RWMutex
	modelMap  map[string]*Endpoint // model name → endpoint that hosts it
}

// NewClient creates a client with the given endpoints.
func NewClient(endpoints []Endpoint) *Client {
	return &Client{
		endpoints: endpoints,
		http: &http.Client{}, // no timeout — streaming responses can run indefinitely; cancellation via context
		modelMap: make(map[string]*Endpoint),
	}
}

// Endpoints returns the configured endpoints.
func (c *Client) Endpoints() []Endpoint {
	return c.endpoints
}

// EndpointStatus represents the health of an endpoint.
type EndpointStatus struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Default bool   `json:"default,omitempty"`
	Online  bool   `json:"online"`
	Error   string `json:"error,omitempty"`
}

// Health checks all endpoints concurrently and returns their status.
func (c *Client) Health(ctx context.Context) []EndpointStatus {
	statuses := make([]EndpointStatus, len(c.endpoints))
	var wg sync.WaitGroup
	for i, ep := range c.endpoints {
		wg.Add(1)
		go func(i int, ep Endpoint) {
			defer wg.Done()
			s := EndpointStatus{Name: ep.Name, URL: ep.URL, Default: ep.Default}
			req, _ := http.NewRequestWithContext(ctx, "GET", ep.URL+"/api/tags", nil)
			resp, err := c.http.Do(req)
			if err != nil {
				s.Error = err.Error()
			} else {
				resp.Body.Close()
				s.Online = resp.StatusCode == 200
				if !s.Online {
					s.Error = fmt.Sprintf("status %d", resp.StatusCode)
				}
			}
			statuses[i] = s
		}(i, ep)
	}
	wg.Wait()
	return statuses
}

// ListModels returns all models across all endpoints.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	type ollamaModel struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		ModifiedAt string `json:"modified_at"`
	}
	type tagResp struct {
		Models []ollamaModel `json:"models"`
	}

	var all []Model
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	for _, ep := range c.endpoints {
		wg.Add(1)
		go func(ep Endpoint) {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "GET", ep.URL+"/api/tags", nil)
			resp, err := c.http.Do(req)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", ep.Name, err)
				}
				mu.Unlock()
				return
			}
			defer resp.Body.Close()
			var tags tagResp
			if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
				return
			}
			mu.Lock()
			for _, m := range tags.Models {
				all = append(all, Model{
					Name:       m.Name,
					Size:       m.Size,
					Endpoint:   ep.Name,
					ModifiedAt: m.ModifiedAt,
				})
			}
			mu.Unlock()
		}(ep)
	}
	wg.Wait()

	// Rebuild model→endpoint cache
	c.mu.Lock()
	c.modelMap = make(map[string]*Endpoint, len(all))
	for _, m := range all {
		for i := range c.endpoints {
			if c.endpoints[i].Name == m.Endpoint {
				c.modelMap[m.Name] = &c.endpoints[i]
				break
			}
		}
	}
	c.mu.Unlock()

	return all, firstErr
}

// resolveEndpoint finds the endpoint that hosts a model.
// Uses the cached model→endpoint mapping from ListModels, falls back to default.
func (c *Client) resolveEndpoint(model string) (*Endpoint, error) {
	if len(c.endpoints) == 0 {
		return nil, fmt.Errorf("no ollama endpoints configured")
	}
	// Check cache first
	c.mu.RLock()
	if ep, ok := c.modelMap[model]; ok {
		c.mu.RUnlock()
		return ep, nil
	}
	cached := len(c.modelMap) > 0
	c.mu.RUnlock()
	// If cache is empty, populate it
	if !cached {
		c.ListModels(context.Background())
		c.mu.RLock()
		if ep, ok := c.modelMap[model]; ok {
			c.mu.RUnlock()
			return ep, nil
		}
		c.mu.RUnlock()
	}
	// Fall back to default endpoint
	for i := range c.endpoints {
		if c.endpoints[i].Default {
			return &c.endpoints[i], nil
		}
	}
	return &c.endpoints[0], nil
}

// Chat sends a chat request and streams the response. The callback is called
// for each chunk. If stream is false, the callback is called once with the
// full response.
func (c *Client) Chat(ctx context.Context, req ChatRequest, onChunk func(ChatResponse)) error {
	ep, err := c.resolveEndpoint(req.Model)
	if err != nil {
		return err
	}

	// Build Ollama-native request.
	// Prepend system message if provided — Ollama /api/chat expects it
	// as a message with role "system", not a top-level field.
	messages := req.Messages
	if req.System != "" {
		messages = append([]ChatMessage{{Role: "system", Content: req.System}}, messages...)
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   req.Stream,
	}
	// Build options: start with caller-provided options, then overlay our defaults.
	opts := map[string]any{}
	for k, v := range req.Options {
		opts[k] = v
	}
	if req.Temperature != nil {
		opts["temperature"] = *req.Temperature
	}
	numPredict := -1 // unlimited by default
	if req.MaxTokens > 0 {
		numPredict = req.MaxTokens
	}
	opts["num_predict"] = numPredict
	body["options"] = opts
	if req.Think != nil {
		body["think"] = *req.Think
	}

	jsonBody, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.URL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s: %w", ep.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: status %d: %s", ep.Name, resp.StatusCode, string(errBody))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var chunk ChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		onChunk(chunk)
		if chunk.Done {
			return nil
		}
	}
}

// Generate sends a single-shot generation request and streams the response.
func (c *Client) Generate(ctx context.Context, req GenerateRequest, onChunk func(GenerateResponse)) error {
	ep, err := c.resolveEndpoint(req.Model)
	if err != nil {
		return err
	}

	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
		"stream": req.Stream,
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature != nil {
		body["options"] = map[string]any{"temperature": *req.Temperature}
	}
	{
		numPredict := -1 // unlimited by default
		if req.MaxTokens > 0 {
			numPredict = req.MaxTokens
		}
		opts, _ := body["options"].(map[string]any)
		if opts == nil {
			opts = map[string]any{}
		}
		opts["num_predict"] = numPredict
		body["options"] = opts
	}
	if req.Think != nil {
		body["think"] = *req.Think
	}

	jsonBody, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", ep.URL+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s: %w", ep.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: status %d: %s", ep.Name, resp.StatusCode, string(errBody))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var chunk GenerateResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		onChunk(chunk)
		if chunk.Done {
			return nil
		}
	}
}
