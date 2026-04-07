package tmux

import (
	"fmt"
	"strings"
	"time"
)

// Provider constants for supported AI CLI tools.
const (
	ProviderClaude = "claude"
	ProviderGemini = "gemini"
	ProviderCodex  = "codex"
)

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

	// Rename the first window to provider name so Agent Dashboard can detect it.
	// Use session-relative window selector instead of hardcoded index — base-index
	// may be 0 or 1 depending on user tmux config.
	b.run("rename-window", "-t", cfg.Name+":^", provider)

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

		// Check ready FIRST — with --no-alt-screen, dismissed trust
		// prompts linger in scrollback and would otherwise cause the
		// trust handler to loop forever.
		if isAgentReady(output, provider) {
			// Small settle delay so the input box has a tick to focus
			// before we start streaming keys into it.
			time.Sleep(500 * time.Millisecond)
			sendLongPrompt(b, target, prompt)
			time.Sleep(200 * time.Millisecond)
			b.run("send-keys", "-t", target, "Enter")
			return
		}

		// Not ready — try dismissing any trust/setup prompt.
		if handleTrustPrompt(b, target, output, provider) {
			time.Sleep(2 * time.Second)
		}
	}
}

// sendLongPrompt chunks a long prompt into multiple send-keys calls.
// Tmux send-keys has practical limits on argument length and very long
// inputs can be truncated or corrupted. Sending in chunks with brief
// delays gives the terminal time to process each piece.
func sendLongPrompt(b *ExecBridge, target, prompt string) {
	const chunkSize = 1024
	if len(prompt) <= chunkSize {
		b.run("send-keys", "-t", target, "-l", prompt)
		return
	}
	for i := 0; i < len(prompt); i += chunkSize {
		end := i + chunkSize
		if end > len(prompt) {
			end = len(prompt)
		}
		b.run("send-keys", "-t", target, "-l", prompt[i:end])
		time.Sleep(20 * time.Millisecond)
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
		// Codex: model upgrade picker ("Try new model" / "Use existing model")
		if strings.Contains(output, "Choose how you'd like Codex to proceed") || strings.Contains(output, "Try new model") {
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
		// Codex with --no-alt-screen: the "% left" token in the status line
		// under the input box only renders after the input box is fully
		// initialized and ready to accept keystrokes. Checking only for
		// "model:" races the input box and causes prompts to be dropped.
		return strings.Contains(output, "% left") && !strings.Contains(output, "Press enter to continue")
	default: // claude
		return strings.Contains(output, "❯") || strings.Contains(output, "Type")
	}
}
