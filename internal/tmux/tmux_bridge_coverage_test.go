package tmux

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ResizeWindow ---

func TestResizeWindow_CallsSetOptionAndResizeWindow(t *testing.T) {
	b, s := bridgeWith()
	require.NoError(t, b.ResizeWindow("alpha:1", 200, 50))

	// Should have 2 calls: set-option then resize-window
	require.Len(t, s.calls, 2)

	// First call: set window-size to manual
	setOpt := s.calls[0].Args
	assert.Equal(t, "set-option", setOpt[0])
	assert.Contains(t, setOpt, "window-size")
	assert.Contains(t, setOpt, "manual")

	// Second call: resize-window with correct dimensions
	resize := s.calls[1].Args
	assert.Equal(t, "resize-window", resize[0])
	assert.Contains(t, resize, "-t")
	assert.Contains(t, resize, "alpha:1")
	assert.Contains(t, resize, "-x")
	assert.Contains(t, resize, "200")
	assert.Contains(t, resize, "-y")
	assert.Contains(t, resize, "50")
}

func TestResizeWindow_PropagatesResizeError(t *testing.T) {
	b, s := bridgeWith()
	s.errorFor["resize-window"] = true
	s.outputs["resize-window"] = "no such window"

	err := b.ResizeWindow("ghost:1", 80, 24)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resize-window")
	assert.Contains(t, err.Error(), "no such window")
}

func TestResizeWindow_SetOptionFailureDoesNotPreventResize(t *testing.T) {
	b, s := bridgeWith()
	// set-option fails but resize-window should still be attempted
	s.errorFor["set-option"] = true

	err := b.ResizeWindow("alpha:1", 100, 30)
	// resize-window itself didn't fail, so no error
	require.NoError(t, err)
	// Both calls should have been made
	require.Len(t, s.calls, 2)
}

// --- sendLongPrompt ---

func TestSendLongPrompt_ShortPrompt_SingleSendKeys(t *testing.T) {
	b, s := bridgeWith()
	sendLongPrompt(b, "t:1.1", "short prompt")

	require.Len(t, s.calls, 1)
	args := s.calls[0].Args
	assert.Equal(t, "send-keys", args[0])
	assert.Contains(t, args, "-l")
	assert.Contains(t, args, "short prompt")
}

func TestSendLongPrompt_ReplacesNewlinesWithSpaces(t *testing.T) {
	b, s := bridgeWith()
	sendLongPrompt(b, "t:1.1", "line one\nline two\nline three")

	require.Len(t, s.calls, 1)
	// The prompt should have newlines replaced and collapsed
	sent := s.calls[0].Args[len(s.calls[0].Args)-1]
	assert.NotContains(t, sent, "\n")
	assert.Contains(t, sent, "line one line two line three")
}

func TestSendLongPrompt_CollapsesConsecutiveSpaces(t *testing.T) {
	b, s := bridgeWith()
	sendLongPrompt(b, "t:1.1", "hello\n\n\nworld")

	require.Len(t, s.calls, 1)
	sent := s.calls[0].Args[len(s.calls[0].Args)-1]
	assert.NotContains(t, sent, "  ") // no double spaces
}

func TestSendLongPrompt_LongPrompt_Chunked(t *testing.T) {
	b, s := bridgeWith()
	// Create a prompt > 1024 chars
	prompt := strings.Repeat("x", 2500)
	sendLongPrompt(b, "t:1.1", prompt)

	// Should be ceil(2500/1024) = 3 chunks
	require.Len(t, s.calls, 3)

	// Verify each chunk uses send-keys -l
	for _, c := range s.calls {
		assert.Equal(t, "send-keys", c.Args[0])
		assert.Contains(t, c.Args, "-l")
	}

	// Verify total content length matches
	total := 0
	for _, c := range s.calls {
		total += len(c.Args[len(c.Args)-1])
	}
	assert.Equal(t, 2500, total)
}

func TestSendLongPrompt_ExactlyChunkSize(t *testing.T) {
	b, s := bridgeWith()
	prompt := strings.Repeat("a", 1024)
	sendLongPrompt(b, "t:1.1", prompt)

	// Exactly 1024 chars — should be a single send (not chunked)
	require.Len(t, s.calls, 1)
}

// --- isAgentReady edge cases ---

func TestIsAgentReady_EmptyOutput(t *testing.T) {
	assert.False(t, isAgentReady("", ProviderClaude))
	assert.False(t, isAgentReady("", ProviderGemini))
	assert.False(t, isAgentReady("", ProviderCodex))
}

func TestIsAgentReady_CodexPercentLeftReady(t *testing.T) {
	output := "codex 1.2.0  o4-mini  78% left"
	assert.True(t, isAgentReady(output, ProviderCodex))
}

func TestIsAgentReady_CodexTrustBlocksReady(t *testing.T) {
	output := "codex 1.2.0\nDo you trust the contents of this directory?"
	assert.False(t, isAgentReady(output, ProviderCodex))
}

func TestIsAgentReady_CodexPressEnterBlocksReady(t *testing.T) {
	output := "model: gpt-5-codex  90% left\nPress enter to continue"
	assert.False(t, isAgentReady(output, ProviderCodex))
}

func TestIsAgentReady_GeminiTrustBlocksReady(t *testing.T) {
	output := "Do you trust the files in this folder?\nType your message"
	assert.False(t, isAgentReady(output, ProviderGemini))
}

func TestIsAgentReady_ClaudeBypassPermissionsNotReady(t *testing.T) {
	// Bypass permissions dialog also has chrome — should NOT be ready
	output := "───────\nDo you trust this?\nEnter to confirm"
	assert.False(t, isAgentReady(output, ProviderClaude))
}

func TestIsAgentReady_ClaudeNoChromeNotReady(t *testing.T) {
	// Just a prompt without the chrome separator line
	output := "❯ "
	assert.False(t, isAgentReady(output, ProviderClaude))
}

func TestIsAgentReady_UnknownProviderTreatedAsClaude(t *testing.T) {
	output := "───────\n❯ \n"
	assert.True(t, isAgentReady(output, "unknown-provider"))
}

// --- handleTrustPrompt additional cases ---

func TestHandleTrustPrompt_ClaudeBypassPermissions(t *testing.T) {
	b, s := bridgeWith()
	output := "Bypass Permissions mode\nNo, exit"
	handled := handleTrustPrompt(b, "a:1.1", output, ProviderClaude)
	assert.True(t, handled)

	// Should send Down (to select Yes) then Enter
	downCount := 0
	enterCount := 0
	for _, c := range s.calls {
		if len(c.Args) >= 3 && c.Args[0] == "send-keys" {
			last := c.Args[len(c.Args)-1]
			if last == "Down" {
				downCount++
			}
			if last == "Enter" {
				enterCount++
			}
		}
	}
	assert.Equal(t, 1, downCount, "should send Down to navigate away from 'No, exit'")
	assert.Equal(t, 1, enterCount, "should send Enter to confirm")
}

func TestHandleTrustPrompt_GeminiTrustFolder(t *testing.T) {
	b, s := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Trust folder", ProviderGemini)
	assert.True(t, handled)

	hasEnter := false
	for _, c := range s.calls {
		if len(c.Args) >= 3 && c.Args[len(c.Args)-1] == "Enter" {
			hasEnter = true
		}
	}
	assert.True(t, hasEnter)
}

func TestHandleTrustPrompt_CodexPressEnter(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "Press enter to continue", ProviderCodex)
	assert.True(t, handled)
}

func TestHandleTrustPrompt_CodexTryNewModel(t *testing.T) {
	b, _ := bridgeWith()
	// Just "Try new model" without "Choose how" should also trigger
	handled := handleTrustPrompt(b, "a:1.1", "Try new model", ProviderCodex)
	assert.True(t, handled)
}

func TestHandleTrustPrompt_GeminiNoPromptReturnsFalse(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "normal output", ProviderGemini)
	assert.False(t, handled)
}

func TestHandleTrustPrompt_CodexNoPromptReturnsFalse(t *testing.T) {
	b, _ := bridgeWith()
	handled := handleTrustPrompt(b, "a:1.1", "normal codex output", ProviderCodex)
	assert.False(t, handled)
}

// --- isInputEmpty additional cases ---

func TestIsInputEmpty_GeminiWithInput(t *testing.T) {
	output := "Type your message\r\n❯ some text"
	assert.False(t, isInputEmpty(output, ProviderGemini))
}

func TestIsInputEmpty_ClaudeMultiplePromptLinesAllEmpty(t *testing.T) {
	output := "───────\n❯ \n───────\n❯  \n"
	assert.True(t, isInputEmpty(output, ProviderClaude))
}

func TestIsInputEmpty_ClaudeNoPromptLine(t *testing.T) {
	// No ❯ at all — should be "empty" (no text typed)
	output := "some output without prompt marker"
	assert.True(t, isInputEmpty(output, ProviderClaude))
}
