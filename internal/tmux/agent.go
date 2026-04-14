package tmux

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// validModelName matches safe model identifiers: alphanumeric, colons, dots,
// slashes, hyphens, underscores, at-signs. Rejects shell metacharacters.
var validModelName = regexp.MustCompile(`^[a-zA-Z0-9.:/_@-]+$`)

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
	Isolation                string `json:"isolation,omitempty"` // "worktree" = auto-create git worktree for isolated work
	DangerousSkipPermissions bool   `json:"dangerouslySkipPermissions,omitempty"`
	CardID                   int64  `json:"cardId,omitempty"`    // kanban card this agent was dispatched from (0 = none)
	Background               bool   `json:"background,omitempty"` // don't steal focus on the frontend when attaching
}

// AgentResult is returned after launching an agent.
type AgentResult struct {
	SessionName  string `json:"sessionName"`
	Target       string `json:"target"`
	Pane         Pane   `json:"pane"`
	WorktreeDir  string `json:"worktreeDir,omitempty"`  // populated when isolation=worktree
	Branch       string `json:"branch,omitempty"`       // worktree branch name
}

// LaunchAgent creates a new tmux session and runs the agent command in it.
func (b *ExecBridge) LaunchAgent(cfg AgentConfig) (*AgentResult, error) {
	if cfg.Name == "" {
		cfg.Name = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	// Validate model name to prevent command injection via shell metacharacters.
	if err := ValidateModelName(cfg.Model); err != nil {
		return nil, err
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

	// Worktree isolation: create a git worktree so all work happens on an
	// isolated branch. The worktree is left after completion for the user
	// to merge/cherry-pick at their discretion.
	var worktreeDir, branch string
	if cfg.Isolation == "worktree" {
		var err error
		worktreeDir, branch, err = createWorktree(dir, cfg.Name)
		if err != nil {
			return nil, fmt.Errorf("create worktree: %w", err)
		}
		dir = worktreeDir
		slog.Info("worktree created", "dir", worktreeDir, "branch", branch, "agent", cfg.Name)
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
		WorktreeDir: worktreeDir,
		Branch:      branch,
	}, nil
}

// createWorktree creates a git worktree for isolated agent work. It resolves
// the git root from the given directory, creates a .worktrees/<name> directory
// with a new branch named card-<name>.
func createWorktree(dir, name string) (worktreeDir, branch string, err error) {
	// Resolve git root
	gitRoot, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", fmt.Errorf("not a git repo (from %s): %w", dir, err)
	}
	root := strings.TrimSpace(string(gitRoot))

	branch = "card-" + name
	worktreeDir = filepath.Join(root, ".worktrees", name)

	out, err := exec.Command("git", "-C", root, "worktree", "add", worktreeDir, "-b", branch).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return worktreeDir, branch, nil
}

// ValidateModelName checks that a model name contains only safe characters.
// Returns an error if the name contains shell metacharacters.
func ValidateModelName(model string) error {
	if model != "" && !validModelName.MatchString(model) {
		return fmt.Errorf("invalid model name %q — only alphanumeric, colons, dots, slashes, hyphens, underscores, and @ are allowed", model)
	}
	return nil
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
	slog.Info("waitAndSendPrompt: starting", "target", target, "provider", provider, "promptLen", len(prompt))
	for i := 0; i < 25; i++ {
		time.Sleep(1 * time.Second)

		output, err := b.CapturePanePlain(target, 30)
		if err != nil {
			slog.Warn("waitAndSendPrompt: capture failed", "iter", i, "target", target, "err", err)
			continue
		}

		ready := isAgentReady(output, provider)

		// Check ready FIRST — with --no-alt-screen, dismissed trust
		// prompts linger in scrollback and would otherwise cause the
		// trust handler to loop forever.
		if ready {
			// Settle delay: the TUI may have painted before its stdin
			// reader is fully initialized. Wait long enough that keystrokes
			// won't be dropped by the PTY before Claude reads them.
			time.Sleep(2 * time.Second)

			// Send the prompt, then verify it actually landed in the input
			// box. If the capture still shows an empty input (❯  followed
			// by nothing), re-send up to 3 times before giving up.
			for sendAttempt := 0; sendAttempt < 3; sendAttempt++ {
				slog.Info("waitAndSendPrompt: sending prompt", "target", target, "promptLen", len(prompt), "sendAttempt", sendAttempt)
				sendLongPrompt(b, target, prompt)
				time.Sleep(500 * time.Millisecond)
				check, err := b.CapturePanePlain(target, 5)
				if err == nil && !isInputEmpty(check, provider) {
					slog.Info("waitAndSendPrompt: prompt landed in input", "sendAttempt", sendAttempt)
					break
				}
				slog.Warn("waitAndSendPrompt: prompt not visible in input, retrying", "sendAttempt", sendAttempt)
				time.Sleep(1 * time.Second)
			}

			// Retry loop: send Enter, then verify the prompt was actually
			// submitted. Long multiline prompts can still be buffering when
			// the first Enter fires, leaving the input box unchanged.
			// We detect this by checking whether the ready-state signal is
			// still present (meaning output hasn't changed — prompt still
			// sitting in the input box).
			for attempt := 0; attempt < 3; attempt++ {
				time.Sleep(300 * time.Millisecond)
				b.run("send-keys", "-t", target, "Enter")
				time.Sleep(1 * time.Second)
				confirmed, err := b.CapturePanePlain(target, 10)
				if err != nil {
					break
				}
				// If the agent is no longer showing "ready" state, the
				// prompt was submitted and it's thinking/working.
				if !isAgentReady(confirmed, provider) {
					break
				}
			}
			return
		}

		// Not ready — try dismissing any trust/setup prompt.
		if handleTrustPrompt(b, target, output, provider) {
			time.Sleep(2 * time.Second)
		}
	}
	slog.Warn("waitAndSendPrompt: exhausted retries without sending prompt", "target", target)
}

// sendLongPrompt sends a prompt to an agent pane via tmux send-keys.
// Newlines are replaced with spaces because Claude Code's input treats
// literal \n as Enter (submit), which would split the prompt into multiple
// premature submissions. The prompt is chunked to avoid tmux argument limits.
func sendLongPrompt(b *ExecBridge, target, prompt string) {
	// Replace newlines with spaces to prevent premature submission
	prompt = strings.ReplaceAll(prompt, "\n", " ")
	// Collapse consecutive spaces
	for strings.Contains(prompt, "  ") {
		prompt = strings.ReplaceAll(prompt, "  ", " ")
	}

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

// IsInputEmpty is the exported wrapper for isInputEmpty.
func IsInputEmpty(output, provider string) bool { return isInputEmpty(output, provider) }

// isInputEmpty returns true if the pane shows an empty input box — i.e., the
// agent is in the ready state but no prompt text has been typed yet.
func isInputEmpty(output, provider string) bool {
	switch provider {
	case ProviderGemini:
		return strings.Contains(output, "Type your message") && !strings.Contains(output, "Type your message\r\n❯")
	case ProviderCodex:
		return false // codex doesn't have a visible input echo we can check
	default: // claude
		// Claude's input line looks like "❯ <text>" when text is present,
		// or "❯  " (with trailing spaces / nothing) when empty.
		// A rough proxy: if every line containing ❯ has nothing after it.
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimRight(line, "\r ")
			if idx := strings.Index(line, "❯"); idx >= 0 {
				after := strings.TrimSpace(line[idx+len("❯"):])
				if after != "" {
					return false // has text
				}
			}
		}
		return true
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
		// Bypass Permissions confirmation: default is "No, exit" — arrow down to "Yes" first
		if strings.Contains(output, "Bypass Permissions mode") && strings.Contains(output, "No, exit") {
			b.run("send-keys", "-t", target, "Down")
			time.Sleep(300 * time.Millisecond)
			b.run("send-keys", "-t", target, "Enter")
			return true
		}
		if strings.Contains(output, "Enter to confirm") {
			b.run("send-keys", "-t", target, "Enter")
			return true
		}
	}
	return false
}

// IsAgentReady checks if the agent CLI is showing its input prompt
// and NOT showing a trust/setup dialog. Exported for use by the supervisor.
func IsAgentReady(output, provider string) bool {
	return isAgentReady(output, provider)
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
		// Must see the Claude Code UI chrome (separator line) alongside the
		// input prompt — the shell prompt also contains ❯ (starship theme)
		// which would otherwise trigger a false positive before claude starts.
		hasChromeUI := strings.Contains(output, "───────")
		hasPrompt := strings.Contains(output, "❯") || strings.Contains(output, "Type")
		// Trust/permission dialogs also contain ─ borders and ❯; exclude them.
		hasTrustDialog := strings.Contains(output, "Enter to confirm") || strings.Contains(output, "Do you trust")
		return hasChromeUI && hasPrompt && !hasTrustDialog
	}
}
