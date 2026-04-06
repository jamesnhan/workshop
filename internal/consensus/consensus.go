package consensus

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jamesnhan/workshop/internal/tmux"
)

// AgentSpec defines one agent in a consensus run.
type AgentSpec struct {
	Name     string `json:"name"`
	Model    string `json:"model"`              // opus, sonnet, haiku, pro, flash, etc.
	Provider string `json:"provider,omitempty"` // claude (default), gemini, codex
}

// ConsensusRequest is the input for a consensus run.
type ConsensusRequest struct {
	Prompt    string      `json:"prompt"`
	Agents    []AgentSpec `json:"agents"`
	Directory string      `json:"directory"`
	Timeout   int         `json:"timeout"` // seconds, default 300 (5 min)
}

// AgentOutput is one agent's captured result.
type AgentOutput struct {
	Name          string `json:"name"`
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	Target        string `json:"target"`
	Output        string `json:"output"`
	Status        string `json:"status"`        // running, needs_input, completed, timeout, error
	NeedsInput    bool   `json:"needsInput"`    // true if waiting for permission/input
	InputPrompt   string `json:"inputPrompt"`   // the prompt text if needs_input
}

// ConsensusResult is the final output.
type ConsensusResult struct {
	ID              string         `json:"id"`
	Prompt          string         `json:"prompt"`
	AgentOutputs    []AgentOutput  `json:"agentOutputs"`
	CoordinatorPane string         `json:"coordinatorPane"`
	Status          string         `json:"status"` // running, collecting, synthesizing, done, error
}

// Engine orchestrates consensus runs.
type Engine struct {
	bridge tmux.Bridge
	logger *slog.Logger
	mu     sync.Mutex
	runs   map[string]*ConsensusResult
}

func NewEngine(bridge tmux.Bridge, logger *slog.Logger) *Engine {
	return &Engine{
		bridge: bridge,
		logger: logger,
		runs:   make(map[string]*ConsensusResult),
	}
}

// StartRun kicks off a consensus run in the background.
func (e *Engine) StartRun(req ConsensusRequest) (*ConsensusResult, error) {
	if len(req.Agents) == 0 {
		return nil, fmt.Errorf("at least one agent is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if req.Timeout <= 0 {
		req.Timeout = 300
	}

	id := fmt.Sprintf("consensus-%d", time.Now().UnixMilli())
	result := &ConsensusResult{
		ID:     id,
		Prompt: req.Prompt,
		Status: "running",
	}

	e.mu.Lock()
	e.runs[id] = result
	e.mu.Unlock()

	go e.runConsensus(id, req)

	return result, nil
}

// GetRun returns the current state of a consensus run.
func (e *Engine) GetRun(id string) *ConsensusResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runs[id]
}

// ListRuns returns all consensus runs.
func (e *Engine) ListRuns() []*ConsensusResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	runs := make([]*ConsensusResult, 0, len(e.runs))
	for _, r := range e.runs {
		runs = append(runs, r)
	}
	return runs
}

func (e *Engine) runConsensus(id string, req ConsensusRequest) {
	e.logger.Info("consensus: starting", "id", id, "agents", len(req.Agents))

	// Phase 1: Launch all agents
	var outputs []AgentOutput
	var targets []string

	for _, spec := range req.Agents {
		name := fmt.Sprintf("%s-%s", id, spec.Name)
		provider := spec.Provider
		if provider == "" {
			provider = tmux.ProviderClaude
		}
		cfg := tmux.AgentConfig{
			Name:      name,
			Provider:  provider,
			Model:     spec.Model,
			Directory: req.Directory,
			Prompt:    req.Prompt,
		}

		result, err := e.bridge.LaunchAgent(cfg)
		if err != nil {
			e.logger.Warn("consensus: agent launch failed", "name", spec.Name, "err", err)
			outputs = append(outputs, AgentOutput{
				Name:     spec.Name,
				Model:    spec.Model,
				Provider: provider,
				Status:   "error",
				Output:   err.Error(),
			})
			continue
		}

		outputs = append(outputs, AgentOutput{
			Name:     spec.Name,
			Model:    spec.Model,
			Provider: provider,
			Target:   result.Target,
			Status:   "running",
		})
		targets = append(targets, result.Target)
		e.logger.Info("consensus: agent launched", "name", spec.Name, "target", result.Target)
	}

	e.mu.Lock()
	e.runs[id].AgentOutputs = outputs
	e.mu.Unlock()

	// Phase 2: Wait for all agents to complete
	e.setStatus(id, "collecting")
	deadline := time.Now().Add(time.Duration(req.Timeout) * time.Second)

	for i := range outputs {
		if outputs[i].Status != "running" {
			continue
		}
		outputs[i].Status = e.waitForCompletion(id, i, outputs[i].Target, outputs[i].Provider, deadline)
		// Capture the output
		captured, err := e.bridge.CapturePanePlain(outputs[i].Target, 1000)
		if err != nil {
			e.logger.Warn("consensus: capture failed", "target", outputs[i].Target, "err", err)
			captured = "(capture failed)"
		}
		outputs[i].Output = captured
		e.logger.Info("consensus: agent finished", "name", outputs[i].Name, "status", outputs[i].Status)
	}

	e.mu.Lock()
	e.runs[id].AgentOutputs = outputs
	e.mu.Unlock()

	// Phase 3: Launch coordinator to synthesize
	e.setStatus(id, "synthesizing")

	coordinatorPrompt := e.buildCoordinatorPrompt(req.Prompt, outputs)
	coordName := fmt.Sprintf("%s-coordinator", id)
	coordCfg := tmux.AgentConfig{
		Name:      coordName,
		Model:     "opus",
		Directory: req.Directory,
		Prompt:    coordinatorPrompt,
	}

	coordResult, err := e.bridge.LaunchAgent(coordCfg)
	if err != nil {
		e.logger.Error("consensus: coordinator launch failed", "err", err)
		e.setStatus(id, "error")
		return
	}

	e.mu.Lock()
	e.runs[id].CoordinatorPane = coordResult.Target
	e.mu.Unlock()

	e.logger.Info("consensus: coordinator launched", "target", coordResult.Target)
	e.setStatus(id, "done")
}

// completionPatterns are Claude Code's task completion timing messages.
var claudeCompletionPatterns = []string{
	"worked for",
	"baked for",
	"sautéed for",
	"cogitated for",
	"pollinated for",
	"cooled for",
	"charred for",
	"simmered for",
	"stewed for",
	"broiled for",
	"smooshed for",
}

// geminiCompletionPatterns detect Gemini CLI task completion.
var geminiCompletionPatterns = []string{
	"✦", // Gemini success indicator
}

// codexCompletionPatterns detect Codex CLI task completion.
var codexCompletionPatterns = []string{
	"completed in",
	"done in",
}

// completionPatternsForProvider returns the right patterns for a provider.
func completionPatternsForProvider(provider string) []string {
	switch provider {
	case tmux.ProviderGemini:
		return geminiCompletionPatterns
	case tmux.ProviderCodex:
		return codexCompletionPatterns
	default:
		return claudeCompletionPatterns
	}
}

// inputPromptPatterns detect when an agent is waiting for user input.
var inputPromptPatterns = []string{
	"do you want to proceed",
	"esc to cancel",
	"yes\n   2. no",
	"enter to confirm",
	"(y/n)",
	"approve",
}

func (e *Engine) waitForCompletion(id string, agentIdx int, target, provider string, deadline time.Time) string {
	// Grace period
	time.Sleep(15 * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Second)

		output, err := e.bridge.CapturePanePlain(target, 50)
		if err != nil {
			continue
		}
		lower := strings.ToLower(output)

		// Check for permission/input prompts
		needsInput := false
		inputPrompt := ""
		for _, pattern := range inputPromptPatterns {
			if strings.Contains(lower, pattern) {
				needsInput = true
				// Extract a few lines around the prompt for context
				lines := strings.Split(output, "\n")
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), pattern) {
						start := i - 1
						if start < 0 { start = 0 }
						end := i + 2
						if end > len(lines) { end = len(lines) }
						inputPrompt = strings.Join(lines[start:end], "\n")
						break
					}
				}
				break
			}
		}

		// Update agent status in real-time
		e.mu.Lock()
		if run, ok := e.runs[id]; ok && agentIdx < len(run.AgentOutputs) {
			if needsInput {
				run.AgentOutputs[agentIdx].NeedsInput = true
				run.AgentOutputs[agentIdx].InputPrompt = inputPrompt
				run.AgentOutputs[agentIdx].Status = "needs_input"
			} else {
				run.AgentOutputs[agentIdx].NeedsInput = false
				run.AgentOutputs[agentIdx].InputPrompt = ""
				if run.AgentOutputs[agentIdx].Status == "needs_input" {
					run.AgentOutputs[agentIdx].Status = "running"
				}
			}
		}
		e.mu.Unlock()

		// Check for completion patterns (provider-specific)
		patterns := completionPatternsForProvider(provider)
		for _, pattern := range patterns {
			if strings.Contains(lower, pattern) {
				// For Claude, verify there's a number after (timing message)
				if provider == "" || provider == tmux.ProviderClaude {
					idx := strings.Index(lower, pattern)
					after := lower[idx+len(pattern):]
					afterLen := len(after)
					if afterLen > 20 { afterLen = 20 }
					if afterLen > 0 && strings.ContainsAny(after[:afterLen], "0123456789") {
						return "completed"
					}
				} else {
					return "completed"
				}
			}
		}
	}
	return "timeout"
}

func (e *Engine) buildCoordinatorPrompt(originalPrompt string, outputs []AgentOutput) string {
	var sb strings.Builder
	sb.WriteString("You are a consensus coordinator. Multiple AI agents were given the same prompt and worked independently. ")
	sb.WriteString("Your job is to synthesize their outputs into a single, best-possible result.\n\n")
	sb.WriteString("## Original Prompt\n")
	sb.WriteString(originalPrompt)
	sb.WriteString("\n\n## Agent Outputs\n\n")

	for _, o := range outputs {
		sb.WriteString(fmt.Sprintf("### Agent: %s (model: %s, status: %s)\n", o.Name, o.Model, o.Status))
		if o.Output != "" {
			// Truncate very long outputs
			output := o.Output
			if len(output) > 5000 {
				output = output[:5000] + "\n... (truncated)"
			}
			sb.WriteString(output)
		} else {
			sb.WriteString("(no output)")
		}
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Your Task\n")
	sb.WriteString("1. Compare all agent outputs\n")
	sb.WriteString("2. Identify consensus (what they agree on)\n")
	sb.WriteString("3. Identify disagreements and pick the best approach\n")
	sb.WriteString("4. Produce a final synthesized result that takes the best from each agent\n")
	sb.WriteString("5. Note any concerns or areas where the agents diverged significantly\n")

	return sb.String()
}

func (e *Engine) setStatus(id, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.runs[id]; ok {
		r.Status = status
	}
}
