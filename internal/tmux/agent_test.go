package tmux

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- buildProviderCommand ---

func TestBuildProviderCommand_claudeDefaults(t *testing.T) {
	assert.Equal(t, "claude", buildProviderCommand("claude", "", false))
}

func TestBuildProviderCommand_claudeWithModel(t *testing.T) {
	assert.Equal(t, "claude --model opus", buildProviderCommand("claude", "opus", false))
}

func TestBuildProviderCommand_claudeSkipPermissions(t *testing.T) {
	expected := "claude --dangerously-skip-permissions " + autoApproveSettings
	assert.Equal(t, expected, buildProviderCommand("claude", "", true))
}

func TestBuildProviderCommand_claudeWithModelAndSkip(t *testing.T) {
	expected := "claude --model sonnet --dangerously-skip-permissions " + autoApproveSettings
	assert.Equal(t, expected, buildProviderCommand("claude", "sonnet", true))
}

func TestBuildProviderCommand_gemini(t *testing.T) {
	assert.Equal(t, "gemini", buildProviderCommand(ProviderGemini, "", false))
	assert.Equal(t, "gemini -m pro", buildProviderCommand(ProviderGemini, "pro", false))
	assert.Equal(t, "gemini --yolo", buildProviderCommand(ProviderGemini, "", true))
	assert.Equal(t, "gemini -m flash --yolo", buildProviderCommand(ProviderGemini, "flash", true))
}

func TestBuildProviderCommand_codexDefaults(t *testing.T) {
	// Without skipPerms, codex runs with --full-auto.
	assert.Equal(t, "codex --no-alt-screen --full-auto", buildProviderCommand(ProviderCodex, "", false))
}

func TestBuildProviderCommand_codexWithModel(t *testing.T) {
	assert.Equal(t, "codex --no-alt-screen -m gpt-5-codex --full-auto", buildProviderCommand(ProviderCodex, "gpt-5-codex", false))
}

func TestBuildProviderCommand_codexSkipPermissions(t *testing.T) {
	// --yolo replaces --full-auto (they're mutually exclusive).
	assert.Equal(t, "codex --no-alt-screen --yolo", buildProviderCommand(ProviderCodex, "", true))
}

func TestBuildProviderCommand_unknownFallsBackToClaude(t *testing.T) {
	// Empty / unknown provider defaults to claude in the switch.
	assert.Equal(t, "claude", buildProviderCommand("", "", false))
	assert.Equal(t, "claude --model opus", buildProviderCommand("unknown", "opus", false))
}

// --- ValidateModelName ---

func TestValidateModelName_valid(t *testing.T) {
	valid := []string{
		"gemma4:26b",
		"huihui_ai/gemma-4-abliterated:31b",
		"hf.co/TrevorJS/gemma-4-26B-A4B-it-uncensored-GGUF",
		"claude-opus-4-6",
		"gpt-5.4",
		"",
	}
	for _, m := range valid {
		assert.NoError(t, ValidateModelName(m), "should be valid: %q", m)
	}
}

func TestValidateModelName_rejectsInjection(t *testing.T) {
	invalid := []string{
		"gemma4; rm -rf /",
		"model$(whoami)",
		"model`id`",
		"model|cat /etc/passwd",
		"model && echo pwned",
		"model\nnewline",
		"model name with spaces",
	}
	for _, m := range invalid {
		assert.Error(t, ValidateModelName(m), "should be invalid: %q", m)
	}
}

// --- isInputEmpty ---

func TestIsInputEmpty_claudeEmptyBox(t *testing.T) {
	output := "chrome ─────\n❯  \n"
	assert.True(t, isInputEmpty(output, ProviderClaude))
}

func TestIsInputEmpty_claudeWithText(t *testing.T) {
	output := "chrome ─────\n❯ hello world\n"
	assert.False(t, isInputEmpty(output, ProviderClaude))
}

func TestIsInputEmpty_geminiEmptyBox(t *testing.T) {
	output := "Type your message"
	assert.True(t, isInputEmpty(output, ProviderGemini))
}

func TestIsInputEmpty_codexAlwaysFalse(t *testing.T) {
	// Codex has no visible input echo so we always return false (don't
	// block on verification).
	assert.False(t, isInputEmpty("anything", ProviderCodex))
}

// --- isAgentReady ---

func TestIsAgentReady_claudeReady(t *testing.T) {
	output := "───────\n❯ \n"
	assert.True(t, isAgentReady(output, ProviderClaude))
}

func TestIsAgentReady_claudeTrustDialogNotReady(t *testing.T) {
	output := "───────\n❯ Do you trust the files in this folder?\nEnter to confirm"
	assert.False(t, isAgentReady(output, ProviderClaude))
}

func TestIsAgentReady_claudeShellPromptNotReady(t *testing.T) {
	// Starship-themed shells also contain ❯ but won't have the chrome
	// separator — should NOT register as ready.
	output := "~/repo ❯ "
	assert.False(t, isAgentReady(output, ProviderClaude))
}

func TestIsAgentReady_geminiReady(t *testing.T) {
	output := "stuff\nType your message\n"
	assert.True(t, isAgentReady(output, ProviderGemini))
}

func TestIsAgentReady_codexReady(t *testing.T) {
	output := "model: gpt-5-codex  45% left\n"
	assert.True(t, isAgentReady(output, ProviderCodex))
}

func TestIsAgentReady_codexStillInTrustPromptNotReady(t *testing.T) {
	output := "model: gpt-5-codex  45% left\nPress enter to continue"
	assert.False(t, isAgentReady(output, ProviderCodex))
}

// --- handleTrustPrompt ---

func TestHandleTrustPrompt_claudeDismissesConfirm(t *testing.T) {
	b, s := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Enter to confirm", ProviderClaude)
	assert.True(t, handled)
	// Should have sent an Enter keystroke.
	require := 0
	for _, c := range s.calls {
		if len(c.Args) >= 4 && c.Args[0] == "send-keys" && c.Args[len(c.Args)-1] == "Enter" {
			require++
		}
	}
	assert.Equal(t, 1, require)
}

func TestHandleTrustPrompt_geminiDismissesTrustFolder(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Do you trust the files in this folder?", ProviderGemini)
	assert.True(t, handled)
}

func TestHandleTrustPrompt_codexDismissesTrust(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Do you trust the contents of this directory?", ProviderCodex)
	assert.True(t, handled)
}

func TestHandleTrustPrompt_codexDismissesModelPicker(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Choose how you'd like Codex to proceed\nTry new model", ProviderCodex)
	assert.True(t, handled)
}

func TestHandleTrustPrompt_noPromptReturnsFalse(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "nothing to see here", ProviderClaude)
	assert.False(t, handled)
}

// --- createWorktree ---

func TestCreateWorktree_createsWorktreeAndBranch(t *testing.T) {
	repo := testhelpers.NewGitRepo(t)
	repo.WriteFile("README.md", "hello")
	repo.Commit("initial")

	wtDir, branch, err := createWorktree(repo.Dir, "orchestrate-42")
	require.NoError(t, err)

	assert.Equal(t, "card-orchestrate-42", branch)
	assert.Equal(t, filepath.Join(repo.Dir, ".worktrees", "orchestrate-42"), wtDir)

	// Worktree directory exists
	info, err := os.Stat(wtDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Files from main are present in worktree
	_, err = os.Stat(filepath.Join(wtDir, "README.md"))
	assert.NoError(t, err)
}

func TestCreateWorktree_failsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	_, _, err := createWorktree(dir, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repo")
}

func TestCreateWorktree_failsOnDuplicateBranch(t *testing.T) {
	repo := testhelpers.NewGitRepo(t)
	repo.WriteFile("README.md", "hello")
	repo.Commit("initial")

	_, _, err := createWorktree(repo.Dir, "dup")
	require.NoError(t, err)

	// Second call with same name should fail (branch already exists)
	_, _, err = createWorktree(repo.Dir, "dup")
	assert.Error(t, err)
}
