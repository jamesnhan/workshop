package tmux

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Provider constants for supported AI CLI tools.
const (
	ProviderClaude = "claude"
	ProviderGemini = "gemini"
	ProviderCodex  = "codex"
)

// AvailableProviders returns providers whose CLI tools are installed.
func AvailableProviders() []string {
	providers := []string{}
	for _, p := range []string{ProviderClaude, ProviderGemini, ProviderCodex} {
		if _, err := exec.LookPath(p); err == nil {
			providers = append(providers, p)
		}
	}
	return providers
}

// IsProviderAvailable checks if a provider's CLI tool is installed.
func IsProviderAvailable(provider string) bool {
	_, err := exec.LookPath(provider)
	return err == nil
}

// AgentConfig defines the parameters for launching an agent.
type AgentConfig struct {
	Name                     string `json:"name"`
	Provider                 string `json:"provider,omitempty"`  // claude (default), gemini, codex
	Directory                string `json:"directory,omitempty"`
	Command                  string `json:"command,omitempty"`   // full command to run (overrides provider defaults)
	Prompt                   string `json:"prompt,omitempty"`    // initial prompt — typed into the agent's input field
	Model                    string `json:"model,omitempty"`     // model flag (--model / -m)
	DangerousSkipPermissions bool   `json:"dangerouslySkipPermissions,omitempty"`
}

// AgentResult is returned after launching an agent.
type AgentResult struct {
	SessionName string `json:"sessionName"`
	Target      string `json:"target"`
	Pane        Pane   `json:"pane"`
}

// LaunchAgent creates a new tmux session and runs the agent command in it.
func (b *ExecBridge) LaunchAgent(cfg AgentConfig) (*AgentResult, error) {
	if cfg.Name == "" {
		cfg.Name = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	provider := cfg.Provider
	if provider == "" {
		provider = ProviderClaude
	}

	cmd := cfg.Command
	if cmd == "" {
		cmd = buildProviderCommand(provider, cfg.Model, cfg.DangerousSkipPermissions)
	}

	dir := cfg.Directory
	if dir == "" {
		dir = "~"
	}

	if err := b.CreateSession(cfg.Name, dir); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Rename window to provider name so Agent Dashboard can detect it
	if _, err := b.run("rename-window", "-t", cfg.Name+":0", provider); err != nil {
		return nil, fmt.Errorf("rename window: %w", err)
	}

	if err := b.SendKeys(cfg.Name, cmd); err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	panes, err := b.ListPanes(cfg.Name)
	if err != nil || len(panes) == 0 {
		return nil, fmt.Errorf("list panes: %w", err)
	}

	if cfg.Prompt != "" {
		go b.waitAndSendPrompt(panes[0].Target, cfg.Prompt, provider)
	}

	return &AgentResult{
		SessionName: cfg.Name,
		Target:      panes[0].Target,
		Pane:        panes[0],
	}, nil
}

// buildProviderCommand constructs the CLI command for a given provider.
func buildProviderCommand(provider, model string, skipPerms bool) string {
	switch provider {
	case ProviderGemini:
		cmd := "gemini"
		if model != "" {
			cmd += " -m " + model
		}
		if skipPerms {
			cmd += " --yolo"
		}
		return cmd

	case ProviderCodex:
		cmd := "codex --no-alt-screen"
		if model != "" {
			cmd += " -m " + model
		}
		if skipPerms {
			cmd += " --yolo"
		} else {
			cmd += " --full-auto"
		}
		return cmd

	default: // claude
		cmd := "claude"
		if model != "" {
			cmd += " --model " + model
		}
		if skipPerms {
			cmd += " --dangerously-skip-permissions"
		}
		return cmd
	}
}

// waitAndSendPrompt waits for the agent CLI to be ready, handles any
// trust/setup prompts, then types the user's prompt.
func (b *ExecBridge) waitAndSendPrompt(target, prompt, provider string) {
	for i := 0; i < 25; i++ {
		time.Sleep(1 * time.Second)

		output, err := b.CapturePanePlain(target, 30)
		if err != nil {
			continue
		}

		// Handle trust/setup prompts per provider
		if handleTrustPrompt(b, target, output, provider) {
			time.Sleep(2 * time.Second)
			continue
		}

		// Check if the agent's input prompt is ready
		if isAgentReady(output, provider) {
			b.run("send-keys", "-t", target, "-l", prompt)
			time.Sleep(100 * time.Millisecond)
			b.run("send-keys", "-t", target, "Enter")
			return
		}
	}
}

// handleTrustPrompt detects and dismisses trust/folder prompts for each provider.
// Returns true if a prompt was handled (caller should wait and retry).
func handleTrustPrompt(b *ExecBridge, target, output, provider string) bool {
	switch provider {
	case ProviderGemini:
		// Gemini: "Do you trust the files in this folder?" with numbered options
		if strings.Contains(output, "Do you trust the files") || strings.Contains(output, "Trust folder") {
			// Select option 1 "Trust folder"
			b.run("send-keys", "-t", target, "Enter")
			return true
		}
	case ProviderCodex:
		// Codex: "Do you trust the contents of this directory?" with Enter to continue
		if strings.Contains(output, "Do you trust the contents") || strings.Contains(output, "Press enter to continue") {
			b.run("send-keys", "-t", target, "Enter")
			return true
		}
	default: // claude
		if strings.Contains(output, "Enter to confirm") {
			b.run("send-keys", "-t", target, "Enter")
			return true
		}
	}
	return false
}

// isAgentReady checks if the agent CLI is showing its input prompt
// and NOT showing a trust/setup dialog.
func isAgentReady(output, provider string) bool {
	// If a trust prompt is visible, we're not ready yet
	if strings.Contains(output, "Do you trust") {
		return false
	}

	switch provider {
	case ProviderGemini:
		// Gemini: "Type your message" in the input box
		return strings.Contains(output, "Type your message")
	case ProviderCodex:
		// Codex with --no-alt-screen: ">" prompt line after startup
		// Look for the model info line which appears when ready
		return strings.Contains(output, "model:") && !strings.Contains(output, "Press enter to continue")
	default: // claude
		return strings.Contains(output, "❯") || strings.Contains(output, "Type")
	}
}
