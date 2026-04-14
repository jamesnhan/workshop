package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jamesnhan/workshop/internal/telemetry"
	"github.com/jamesnhan/workshop/internal/tmux"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// mcpPaneTarget caches the resolved pane target for this MCP subprocess.
// Set once during Serve() from $TMUX_PANE.
var mcpPaneTarget string

// traced wraps a tool handler with an OTel span named "mcp.<toolName>".
// When telemetry is disabled, the span is a no-op and the cost is zero.
func traced(toolName string, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, span := telemetry.Tracer("mcp").Start(ctx, "mcp."+toolName,
			telemetry.Attrs(
				attribute.String("workshop.mcp.tool", toolName),
				attribute.String("workshop.pane.target", mcpPaneTarget),
			),
		)
		defer span.End()

		result, err := h(ctx, req)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, "tool returned error")
		}
		return result, err
	}
}

func Serve() {
	bridge := tmux.NewExecBridge("")

	// Initialize OTel for the MCP subprocess (service name: workshop-mcp).
	os.Setenv("OTEL_SERVICE_NAME", "workshop-mcp")
	logger := telemetry.DiscardLogger()
	otelShutdown, _ := telemetry.Init(context.Background(), logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = otelShutdown(ctx)
	}()

	// Resolve this subprocess's pane target for span attributes.
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if out, err := exec.Command("tmux", "display-message", "-p", "-t", pane,
			"#{session_name}:#{window_index}.#{pane_index}").CombinedOutput(); err == nil {
			mcpPaneTarget = strings.TrimSpace(string(out))
		}
	}

	s := server.NewMCPServer("workshop", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("Workshop is a tmux session manager and channel hub. Channel messages from other agent panes arrive as <channel source=\"workshop\" from=\"<sender>\" project=\"<project>\">body</channel>. Read them and act on them as instructions or context from the sending agent."),
		server.WithExperimental(map[string]any{
			"claude/channel": map[string]any{},
		}),
	)

	// Start the channel listener loop in the background. It detects this
	// MCP subprocess's pane target via $TMUX_PANE, registers with the
	// central Workshop server, and emits notifications/claude/channel events
	// to the parent Claude Code on inbound messages.
	go runChannelListener(s)

	// --- Tools ---

	s.AddTool(mcp.NewTool("list_sessions",
		mcp.WithDescription("List all tmux sessions"),
		mcp.WithBoolean("include_hidden", mcp.Description("Include hidden sessions (consensus-*, workshop-ctrl-*). Default: false")),
	), traced("list_sessions", listSessionsHandler(bridge)))

	s.AddTool(mcp.NewTool("list_panes",
		mcp.WithDescription("List all panes in a tmux session"),
		mcp.WithString("session", mcp.Required(), mcp.Description("Session name")),
	), traced("list_panes", listPanesHandler(bridge)))

	s.AddTool(mcp.NewTool("create_session",
		mcp.WithDescription("Create a new tmux session"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("directory", mcp.Description("Starting directory")),
	), traced("create_session", createSessionHandler(bridge)))

	s.AddTool(mcp.NewTool("kill_session",
		mcp.WithDescription("Kill a tmux session"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Session name")),
	), traced("kill_session", killSessionHandler(bridge)))

	s.AddTool(mcp.NewTool("send_keys",
		mcp.WithDescription("Send a command to a tmux pane (appends Enter)"),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 0:1.1)")),
		mcp.WithString("command", mcp.Required(), mcp.Description("Command to send")),
	), traced("send_keys", sendKeysHandler(bridge)))

	s.AddTool(mcp.NewTool("send_text",
		mcp.WithDescription("Send literal text to a tmux pane (no Enter appended)"),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 0:1.1)")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to send")),
	), traced("send_text", sendTextHandler(bridge)))

	s.AddTool(mcp.NewTool("capture_pane",
		mcp.WithDescription("Capture the current visible content of a tmux pane"),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 0:1.1)")),
		mcp.WithNumber("lines", mcp.Description("Number of scrollback lines to capture (default 50)")),
	), traced("capture_pane", capturePaneHandler(bridge)))

	s.AddTool(mcp.NewTool("split_window",
		mcp.WithDescription("Split a tmux window to create a new pane"),
		mcp.WithString("target", mcp.Required(), mcp.Description("Target window/pane to split")),
		mcp.WithBoolean("horizontal", mcp.Description("Split horizontally (default: vertical)")),
	), traced("split_window", splitWindowHandler(bridge)))

	s.AddTool(mcp.NewTool("create_window",
		mcp.WithDescription("Create a new window in a tmux session"),
		mcp.WithString("session", mcp.Required(), mcp.Description("Session name")),
		mcp.WithString("name", mcp.Description("Window name")),
	), traced("create_window", createWindowHandler(bridge)))

	s.AddTool(mcp.NewTool("launch_agent",
		mcp.WithDescription("Launch an AI coding agent in a new tmux session. Supports Claude, Gemini, and Codex. Use 'preset' to launch a specialist agent (reviewer, tester, security, planner, refactorer, architect) with pre-configured role prompts."),
		mcp.WithString("name", mcp.Description("Session name (auto-generated if empty)")),
		mcp.WithString("preset", mcp.Description("Named agent preset to use (e.g. reviewer, tester, security, planner, refactorer, architect). Loads provider, model, and system prompt from the preset. Explicit params override preset values.")),
		mcp.WithString("provider", mcp.Description("AI provider: claude (default), gemini, codex")),
		mcp.WithString("directory", mcp.Description("Working directory")),
		mcp.WithString("prompt", mcp.Description("Initial prompt for the agent")),
		mcp.WithString("model", mcp.Description("Model to use (e.g. opus, sonnet, pro, flash, gpt-5-codex)")),
		mcp.WithString("command", mcp.Description("Full command to run (overrides provider defaults)")),
		mcp.WithBoolean("dangerouslySkipPermissions", mcp.Description("Skip permission prompts (--yolo for gemini/codex)")),
	), traced("launch_agent", launchAgentHandler(bridge)))

	s.AddTool(mcp.NewTool("search_output",
		mcp.WithDescription("Search terminal output history across panes. Requires the Workshop web server to be running."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search string (case-insensitive)")),
		mcp.WithString("target", mcp.Description("Filter to a specific pane target (e.g. workshop:1.1)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	), traced("search_output", searchOutputHandler()))

	s.AddTool(mcp.NewTool("rename_session",
		mcp.WithDescription("Rename a tmux session"),
		mcp.WithString("old_name", mcp.Required(), mcp.Description("Current session name")),
		mcp.WithString("new_name", mcp.Required(), mcp.Description("New session name")),
	), traced("rename_session", renameSessionHandler(bridge)))

	// Kanban tools — these call the Workshop HTTP API
	s.AddTool(mcp.NewTool("kanban_list",
		mcp.WithDescription("List kanban cards. Requires Workshop web server running. Archived (done) cards are hidden by default."),
		mcp.WithString("project", mcp.Description("Filter by project name")),
		mcp.WithBoolean("include_archived", mcp.Description("Include archived cards (default: false)")),
		mcp.WithNumber("limit", mcp.Description("Max cards to return (default 50). Use 0 for all cards.")),
		mcp.WithNumber("offset", mcp.Description("Number of cards to skip (default 0). Use with limit for pagination.")),
	), traced("kanban_list", kanbanListHandler()))

	s.AddTool(mcp.NewTool("kanban_create",
		mcp.WithDescription("Create a kanban card"),
		mcp.WithString("title", mcp.Required(), mcp.Description("Card title")),
		mcp.WithString("description", mcp.Description("Card description")),
		mcp.WithString("column", mcp.Description("Column: backlog, in_progress, review, done (default: backlog)")),
		mcp.WithString("project", mcp.Description("Project name")),
		mcp.WithString("pane_target", mcp.Description("Linked pane target")),
		mcp.WithString("labels", mcp.Description("Comma-separated labels")),
		mcp.WithString("card_type", mcp.Description("Card type: bug, feature, task, chore")),
		mcp.WithString("priority", mcp.Description("Priority: P0, P1, P2, P3")),
		mcp.WithNumber("parent_id", mcp.Description("Parent card ID for hierarchical tasks (0 = no parent)")),
	), traced("kanban_create", kanbanCreateHandler()))

	s.AddTool(mcp.NewTool("kanban_edit",
		mcp.WithDescription("Edit a kanban card. Only provided fields are updated."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Card ID")),
		mcp.WithString("title", mcp.Description("New title")),
		mcp.WithString("description", mcp.Description("New description")),
		mcp.WithString("column", mcp.Description("New column")),
		mcp.WithString("project", mcp.Description("New project")),
		mcp.WithString("pane_target", mcp.Description("New pane target")),
		mcp.WithString("labels", mcp.Description("New labels")),
		mcp.WithString("card_type", mcp.Description("Card type: bug, feature, task, chore")),
		mcp.WithString("priority", mcp.Description("Priority: P0, P1, P2, P3")),
		mcp.WithNumber("parent_id", mcp.Description("Parent card ID for hierarchical tasks (0 = no parent)")),
	), traced("kanban_edit", kanbanEditHandler()))

	s.AddTool(mcp.NewTool("kanban_move",
		mcp.WithDescription("Move a kanban card to a different column"),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Card ID")),
		mcp.WithString("column", mcp.Required(), mcp.Description("Target column: backlog, in_progress, review, done")),
	), traced("kanban_move", kanbanMoveHandler()))

	s.AddTool(mcp.NewTool("kanban_add_note",
		mcp.WithDescription("Add a note to a kanban card. Use this to log progress, decisions, or blockers as you work."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Card ID")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Note text (concise, no fluff)")),
	), traced("kanban_add_note", kanbanAddNoteHandler()))

	s.AddTool(mcp.NewTool("kanban_delete",
		mcp.WithDescription("Delete a kanban card"),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Card ID")),
	), traced("kanban_delete", kanbanDeleteHandler()))

	// Approvals — blocking tool for agent actions that need user sign-off
	s.AddTool(mcp.NewTool("request_approval",
		mcp.WithDescription("Request user approval before proceeding with a significant action. BLOCKING — the tool will not return until the user approves or denies in the Workshop UI (up to 10 min timeout). Use for destructive operations, deployments, or any action that should be reviewed first."),
		mcp.WithString("action", mcp.Required(), mcp.Description("What needs approval: deploy, delete, push, migrate, etc.")),
		mcp.WithString("details", mcp.Required(), mcp.Description("Human-readable description of what will happen if approved")),
		mcp.WithString("project", mcp.Description("Project name")),
		mcp.WithString("diff", mcp.Description("Optional diff or code snippet showing the proposed change")),
	), traced("request_approval", requestApprovalHandler()))

	// Activity log
	s.AddTool(mcp.NewTool("report_activity",
		mcp.WithDescription("Report an agent action to the Workshop activity feed. Use to log significant actions: file writes, commands executed, decisions made, errors encountered. The activity feed gives James visibility into what all agents are doing across panes."),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action type: file_write, command, decision, error, status, deploy, test, review")),
		mcp.WithString("summary", mcp.Required(), mcp.Description("One-line human-readable summary of what happened")),
		mcp.WithString("project", mcp.Description("Project name")),
		mcp.WithString("metadata", mcp.Description("Optional JSON string with structured details (file paths, exit codes, etc.)")),
		mcp.WithNumber("parent_id", mcp.Description("Parent activity ID — use to nest this action under a parent (e.g. subagent work under the spawning agent's entry)")),
	), traced("report_activity", reportActivityHandler()))

	// Task orchestrator — drives a card through phased agent workflow
	s.AddTool(mcp.NewTool("orchestrate_card",
		mcp.WithDescription("Launch an autonomous orchestrator that drives a kanban card through plan→implement→test→review→PR phases. Each phase launches a specialist agent, captures output, and checkpoints with the user via approval gates. The orchestrator runs as a separate Claude agent session."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Kanban card ID to orchestrate")),
		mcp.WithString("directory", mcp.Description("Working directory for all agents (defaults to current directory)")),
		mcp.WithString("isolation", mcp.Description("Set to 'worktree' to auto-create a git worktree so all work happens on an isolated branch. The worktree is kept after completion for you to merge/cherry-pick.")),
	), traced("orchestrate_card", orchestrateCardHandler()))

	s.AddTool(mcp.NewTool("consensus_start",
		mcp.WithDescription("Start a consensus run — multiple agents work on the same prompt, then a coordinator synthesizes. Requires Workshop server."),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("The prompt for all agents")),
		mcp.WithString("directory", mcp.Description("Working directory for agents")),
		mcp.WithString("agents", mcp.Description("Comma-separated agent specs. Formats: 'provider' (e.g. 'codex'), 'provider:model' (e.g. 'claude:opus'), or 'name:provider:model' (e.g. 'deep:gemini:pro'). Provider must be claude/gemini/codex. Default: 3 sonnet claude agents.")),
		mcp.WithNumber("timeout", mcp.Description("Timeout in seconds (default 300)")),
	), traced("consensus_start", consensusStartHandler()))

	s.AddTool(mcp.NewTool("consensus_status",
		mcp.WithDescription("Check the status of a consensus run"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Consensus run ID")),
	), traced("consensus_status", consensusStatusHandler()))

	s.AddTool(mcp.NewTool("consensus_list",
		mcp.WithDescription("List all consensus runs with their status"),
	), traced("consensus_list", consensusListHandler()))

	s.AddTool(mcp.NewTool("consensus_capture",
		mcp.WithDescription("Capture the full output from a specific consensus agent"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Consensus run ID")),
		mcp.WithString("agent", mcp.Required(), mcp.Description("Agent name (e.g. 'reviewer-1' or 'coordinator')")),
		mcp.WithNumber("lines", mcp.Description("Lines to capture (default 200)")),
	), traced("consensus_capture", consensusCaptureHandler()))

	s.AddTool(mcp.NewTool("consensus_review",
		mcp.WithDescription("Collect and display all agent outputs from a consensus run for review. Shows each agent's findings side by side."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Consensus run ID")),
	), traced("consensus_review", consensusReviewHandler()))

	s.AddTool(mcp.NewTool("consensus_cleanup",
		mcp.WithDescription("Kill all tmux sessions for a finished consensus run (agents + coordinator). ALWAYS call this when you're done reviewing a consensus run — the sessions linger otherwise and clutter tmux."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Consensus run ID")),
	), traced("consensus_cleanup", consensusCleanupHandler()))

	s.AddTool(mcp.NewTool("run_config",
		mcp.WithDescription("Run a Lua config script. Requires Workshop web server running."),
		mcp.WithString("path", mcp.Description("Path to workshop.lua file")),
		mcp.WithString("code", mcp.Description("Inline Lua code to execute")),
	), traced("run_config", runConfigHandler()))

	s.AddTool(mcp.NewTool("set_pane_status",
		mcp.WithDescription("Set a status indicator on your pane in Workshop. Use this when you finish a task (green), need user input (yellow), or encounter an error (red)."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 'workshop:1.1')")),
		mcp.WithString("status", mcp.Required(), mcp.Description("Status color: green (done), yellow (needs input), red (error)")),
		mcp.WithString("message", mcp.Description("Short status message")),
	), traced("set_pane_status", setPaneStatusHandler()))

	s.AddTool(mcp.NewTool("clear_pane_status",
		mcp.WithDescription("Clear the status indicator on a pane in Workshop."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 'workshop:1.1')")),
	), traced("clear_pane_status", clearPaneStatusHandler()))

	s.AddTool(mcp.NewTool("open_doc",
		mcp.WithDescription("Open a markdown file in the Workshop Docs view. Switches the UI to the Docs tab and loads the file. Useful for surfacing agent-generated plans, summaries, or notes."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or ~-relative path to a .md/.txt/.yaml/.json/.lua/.toml file")),
	), traced("open_doc", openDocHandler()))

	// --- UI control tools (drive the Workshop frontend from agents) ---

	s.AddTool(mcp.NewTool("show_toast",
		mcp.WithDescription("Show a transient toast notification in the Workshop UI. Use to surface short status messages without blocking."),
		mcp.WithString("message", mcp.Required(), mcp.Description("Toast message text")),
		mcp.WithString("kind", mcp.Description("Visual style: info (default), success, warning, error")),
	), traced("show_toast", uiActionHandler("show_toast", false, []string{"message", "kind"})))

	s.AddTool(mcp.NewTool("switch_view",
		mcp.WithDescription("Switch the Workshop main view tab."),
		mcp.WithString("view", mcp.Required(), mcp.Description("One of: sessions, kanban, docs, agents, settings")),
	), traced("switch_view", uiActionHandler("switch_view", false, []string{"view"})))

	s.AddTool(mcp.NewTool("focus_cell",
		mcp.WithDescription("Focus a specific grid cell in the pane layout by cell ID."),
		mcp.WithString("cellId", mcp.Required(), mcp.Description("Cell ID (e.g. 'cell-3')")),
	), traced("focus_cell", uiActionHandler("focus_cell", false, []string{"cellId"})))

	s.AddTool(mcp.NewTool("focus_pane",
		mcp.WithDescription("Focus the cell currently displaying the given pane target. No-op if the pane isn't in the layout."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target (e.g. 'workshop:1.1')")),
	), traced("focus_pane", uiActionHandler("focus_pane", false, []string{"target"})))

	s.AddTool(mcp.NewTool("assign_pane",
		mcp.WithDescription("Assign a pane to a grid cell, making it the active tab. Defaults to the focused cell if cellId is omitted."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target")),
		mcp.WithString("cellId", mcp.Description("Optional cell ID. Defaults to focused cell.")),
	), traced("assign_pane", uiActionHandler("assign_pane", false, []string{"target", "cellId"})))

	s.AddTool(mcp.NewTool("open_card",
		mcp.WithDescription("Open the kanban view and expand a specific card."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Card ID")),
	), traced("open_card", uiActionHandler("open_card", false, []string{"id"})))

	s.AddTool(mcp.NewTool("prompt_user",
		mcp.WithDescription("Show a themed input dialog and wait for the user's typed response. Blocking — returns the user's input or an error if they cancelled. Use for clarifying questions during a task."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Dialog title")),
		mcp.WithString("message", mcp.Description("Optional helper text below the title")),
		mcp.WithString("initialValue", mcp.Description("Pre-filled input value")),
	), traced("prompt_user", uiActionHandler("prompt_user", true, []string{"title", "message", "initialValue"})))

	s.AddTool(mcp.NewTool("confirm",
		mcp.WithDescription("Show a themed yes/no confirmation dialog. Blocking — returns 'true' if confirmed, 'false' if cancelled. Use for destructive or irreversible actions that need user sign-off."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Dialog title")),
		mcp.WithString("message", mcp.Description("Optional message body")),
		mcp.WithBoolean("danger", mcp.Description("Show the confirm button in danger styling (red)")),
	), traced("confirm", uiActionHandler("confirm", true, []string{"title", "message", "danger"})))

	// --- Channels (inter-pane / inter-agent messaging) ---

	s.AddTool(mcp.NewTool("channel_publish",
		mcp.WithDescription("Publish a message to a Workshop channel. All panes subscribed to the channel will receive the message. Use for inter-agent communication, broadcasting status, or coordinating multi-agent work."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Message body")),
		mcp.WithString("from", mcp.Description("Sender pane target or agent name (optional, helps the receiver know who sent it)")),
		mcp.WithString("project", mcp.Description("Optional project tag — namespaces the channel")),
	), traced("channel_publish", channelPublishHandler()))

	s.AddTool(mcp.NewTool("channel_subscribe",
		mcp.WithDescription("Subscribe a pane to a Workshop channel so it receives published messages."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target to subscribe (e.g. 'workshop:1.1')")),
		mcp.WithString("project", mcp.Description("Optional project tag")),
	), traced("channel_subscribe", channelSubscribeHandler()))

	s.AddTool(mcp.NewTool("channel_unsubscribe",
		mcp.WithDescription("Remove a pane from a Workshop channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Pane target")),
	), traced("channel_unsubscribe", channelUnsubscribeHandler()))

	s.AddTool(mcp.NewTool("channel_list",
		mcp.WithDescription("List all active Workshop channels with their subscriber and message counts."),
		mcp.WithString("project", mcp.Description("Filter by project tag")),
	), traced("channel_list", channelListHandler()))

	s.AddTool(mcp.NewTool("channel_messages",
		mcp.WithDescription("List recent messages on a channel."),
		mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
		mcp.WithNumber("limit", mcp.Description("Max messages to return (default 50, max 500)")),
	), traced("channel_messages", channelMessagesHandler()))

	// Ollama (local LLM)
	s.AddTool(mcp.NewTool("ollama_models",
		mcp.WithDescription("List available local LLM models across all Ollama endpoints."),
	), traced("ollama_models", ollamaModelsHandler()))

	s.AddTool(mcp.NewTool("ollama_chat",
		mcp.WithDescription("Chat with a local Ollama model. Returns the model's response. Use for creative generation, uncensored content, or tasks best handled by a local model."),
		mcp.WithString("model", mcp.Required(), mcp.Description("Model name (e.g. gemma4:26b, cydonia-24b, stheno-8b)")),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("User message to send")),
		mcp.WithString("system", mcp.Description("System prompt (optional)")),
		mcp.WithNumber("temperature", mcp.Description("Sampling temperature (default: model default)")),
		mcp.WithNumber("max_tokens", mcp.Description("Max tokens to generate")),
		mcp.WithBoolean("think", mcp.Description("Enable thinking mode (default: false, recommended for Gemma 4)")),
	), traced("ollama_chat", ollamaChatHandler()))

	s.AddTool(mcp.NewTool("ollama_generate",
		mcp.WithDescription("Single-shot text generation with a local Ollama model. Use for completions, creative writing, or raw text generation without chat format."),
		mcp.WithString("model", mcp.Required(), mcp.Description("Model name")),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("Prompt text")),
		mcp.WithString("system", mcp.Description("System prompt (optional)")),
		mcp.WithNumber("temperature", mcp.Description("Sampling temperature")),
		mcp.WithNumber("max_tokens", mcp.Description("Max tokens to generate")),
		mcp.WithBoolean("think", mcp.Description("Enable thinking mode (default: false)")),
	), traced("ollama_generate", ollamaGenerateHandler()))

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "workshop mcp error: %v\n", err)
		os.Exit(1)
	}
}

// --- Handlers ---

func listSessionsHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		includeHidden := mcp.ParseBoolean(req, "include_hidden", false)
		var sessions []tmux.Session
		var err error
		if includeHidden {
			if eb, ok := bridge.(*tmux.ExecBridge); ok {
				sessions, err = eb.ListAllSessions()
			} else {
				sessions, err = bridge.ListSessions()
			}
		} else {
			sessions, err = bridge.ListSessions()
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(sessions, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}

func listPanesHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := mcp.ParseString(req, "session", "")
		if session == "" {
			return mcp.NewToolResultError("session is required"), nil
		}
		panes, err := bridge.ListPanes(session)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, _ := json.MarshalIndent(panes, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}

func createSessionHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		dir := mcp.ParseString(req, "directory", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		// Route through REST so the frontend receives a session_created
		// broadcast (background=true since MCP isn't user-initiated).
		body := map[string]any{"name": name, "startDir": dir, "background": true}
		raw, _ := json.Marshal(body)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/sessions", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			if err := bridge.CreateSession(name, dir); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Session '%s' created", name)), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			return mcp.NewToolResultError(string(b)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session '%s' created", name)), nil
	}
}

func killSessionHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(req, "name", "")
		if name == "" {
			return mcp.NewToolResultError("name is required"), nil
		}
		if err := bridge.KillSession(name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session '%s' killed", name)), nil
	}
}

func sendKeysHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		command := mcp.ParseString(req, "command", "")
		if target == "" || command == "" {
			return mcp.NewToolResultError("target and command are required"), nil
		}
		if err := bridge.SendKeys(target, command); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Sent to %s: %s", target, command)), nil
	}
}

func sendTextHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		text := mcp.ParseString(req, "text", "")
		if target == "" || text == "" {
			return mcp.NewToolResultError("target and text are required"), nil
		}
		if err := bridge.SendKeysLiteral(target, text); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Sent literal text to %s", target)), nil
	}
}

func capturePaneHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		lines := mcp.ParseInt(req, "lines", 50)
		if target == "" {
			return mcp.NewToolResultError("target is required"), nil
		}
		output, err := bridge.CapturePane(target, lines)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		// Strip ANSI escape codes for cleaner LLM consumption
		clean := stripAnsi(output)
		return mcp.NewToolResultText(clean), nil
	}
}

func splitWindowHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		horizontal := mcp.ParseBoolean(req, "horizontal", false)
		if target == "" {
			return mcp.NewToolResultError("target is required"), nil
		}
		paneID, err := bridge.SplitWindow(target, horizontal)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Created pane: %s", paneID)), nil
	}
}

func createWindowHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := mcp.ParseString(req, "session", "")
		name := mcp.ParseString(req, "name", "")
		if session == "" {
			return mcp.NewToolResultError("session is required"), nil
		}
		if err := bridge.CreateWindow(session, name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Window '%s' created in session '%s'", name, session)), nil
	}
}

func launchAgentHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Resolve preset if provided — fetch from REST API, then let explicit params override.
		provider := mcp.ParseString(req, "provider", "")
		model := mcp.ParseString(req, "model", "")
		prompt := mcp.ParseString(req, "prompt", "")
		directory := mcp.ParseString(req, "directory", "")
		name := mcp.ParseString(req, "name", "")

		if presetName := mcp.ParseString(req, "preset", ""); presetName != "" {
			presetResp, err := http.Get(workshopAPIURL() + "/api/v1/agent-presets")
			if err == nil {
				defer presetResp.Body.Close()
				var presets []map[string]any
				json.NewDecoder(presetResp.Body).Decode(&presets)
				for _, p := range presets {
					if pName, _ := p["name"].(string); pName == presetName {
						if provider == "" { provider, _ = p["provider"].(string) }
						if model == "" { model, _ = p["model"].(string) }
						if directory == "" { directory, _ = p["directory"].(string) }
						// Prepend system prompt to the user's prompt
						if sp, _ := p["systemPrompt"].(string); sp != "" {
							if prompt != "" {
								prompt = sp + "\n\n" + prompt
							} else if pp, _ := p["prompt"].(string); pp != "" {
								prompt = sp + "\n\n" + pp
							} else {
								prompt = sp
							}
						} else if prompt == "" {
							prompt, _ = p["prompt"].(string)
						}
						if name == "" { name = presetName }
						break
					}
				}
			}
		}

		// Route through the REST endpoint so the frontend receives the
		// session_created broadcast. Agent launches from MCP are always
		// background=true — the user isn't directly creating them.
		body := map[string]any{
			"name":                     name,
			"provider":                 provider,
			"directory":                directory,
			"command":                  mcp.ParseString(req, "command", ""),
			"prompt":                   prompt,
			"model":                    model,
			"dangerousSkipPermissions": mcp.ParseBoolean(req, "dangerouslySkipPermissions", false),
			"background":               true,
		}
		raw, _ := json.Marshal(body)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/agents/launch", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			// Fall back to direct bridge call if the web server isn't running.
			execBridge, ok := bridge.(*tmux.ExecBridge)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("agent launch failed: %v", err)), nil
			}
			cfg := tmux.AgentConfig{
				Name:      mcp.ParseString(req, "name", ""),
				Provider:  mcp.ParseString(req, "provider", ""),
				Directory: mcp.ParseString(req, "directory", ""),
				Command:   mcp.ParseString(req, "command", ""),
				Prompt:    mcp.ParseString(req, "prompt", ""),
				Model:     mcp.ParseString(req, "model", ""),
				DangerousSkipPermissions: mcp.ParseBoolean(req, "dangerouslySkipPermissions", false),
			}
			result, ferr := execBridge.LaunchAgent(cfg)
			if ferr != nil {
				return mcp.NewToolResultError(ferr.Error()), nil
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return mcp.NewToolResultText(string(data)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 201 {
			return mcp.NewToolResultError(string(result)), nil
		}
		return mcp.NewToolResultText(string(result)), nil
	}
}

func renameSessionHandler(bridge tmux.Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		oldName := mcp.ParseString(req, "old_name", "")
		newName := mcp.ParseString(req, "new_name", "")
		if oldName == "" || newName == "" {
			return mcp.NewToolResultError("old_name and new_name are required"), nil
		}
		if err := bridge.RenameSession(oldName, newName); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Renamed session '%s' to '%s'", oldName, newName)), nil
	}
}

func workshopAPIURL() string {
	u := os.Getenv("WORKSHOP_API_URL")
	if u == "" {
		return "http://localhost:9090"
	}
	return u
}

func kanbanListHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project := mcp.ParseString(req, "project", "")
		includeArchived := mcp.ParseBoolean(req, "include_archived", false)
		limit := mcp.ParseInt(req, "limit", 50)
		offset := mcp.ParseInt(req, "offset", 0)

		params := url.Values{}
		if project != "" {
			params.Set("project", project)
		}
		if includeArchived {
			params.Set("include_archived", "true")
		}
		params.Set("limit", fmt.Sprintf("%d", limit))
		if offset > 0 {
			params.Set("offset", fmt.Sprintf("%d", offset))
		}
		resp, err := http.Get(workshopAPIURL() + "/api/v1/cards?" + params.Encode())
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var page struct {
			Cards  []map[string]any `json:"cards"`
			Total  int              `json:"total"`
			Limit  int              `json:"limit"`
			Offset int              `json:"offset"`
		}
		json.Unmarshal(body, &page)
		if len(page.Cards) == 0 {
			return mcp.NewToolResultText("No cards found."), nil
		}

		var sb strings.Builder
		end := page.Offset + len(page.Cards)
		fmt.Fprintf(&sb, "Showing %d–%d of %d cards", page.Offset+1, end, page.Total)
		if page.Total > end {
			fmt.Fprintf(&sb, " (next: offset=%d)", end)
		}
		sb.WriteString("\n\n")
		for _, c := range page.Cards {
			fmt.Fprintf(&sb, "[%s] #%.0f %s", c["column"], c["id"], c["title"])
			if p, ok := c["project"].(string); ok && p != "" {
				fmt.Fprintf(&sb, " (%s)", p)
			}
			if t, ok := c["paneTarget"].(string); ok && t != "" {
				fmt.Fprintf(&sb, " → %s", t)
			}
			sb.WriteString("\n")
			if d, ok := c["description"].(string); ok && d != "" {
				fmt.Fprintf(&sb, "  %s\n", d)
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func kanbanCreateHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title := mcp.ParseString(req, "title", "")
		if title == "" {
			return mcp.NewToolResultError("title is required"), nil
		}
		card := map[string]any{
			"title":       title,
			"description": mcp.ParseString(req, "description", ""),
			"column":      mcp.ParseString(req, "column", "backlog"),
			"project":     mcp.ParseString(req, "project", ""),
			"paneTarget":  mcp.ParseString(req, "pane_target", ""),
			"labels":      mcp.ParseString(req, "labels", ""),
			"cardType":    mcp.ParseString(req, "card_type", ""),
			"priority":    mcp.ParseString(req, "priority", ""),
			"parentId":    mcp.ParseInt(req, "parent_id", 0),
		}
		body, _ := json.Marshal(card)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/cards", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 201 {
			return mcp.NewToolResultError(string(result)), nil
		}
		var created map[string]any
		json.Unmarshal(result, &created)
		return mcp.NewToolResultText(fmt.Sprintf("Created card #%.0f: %s", created["id"], title)), nil
	}
}

func kanbanEditHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseInt(req, "id", 0)
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}

		// Fetch current card
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/cards?project=", workshopAPIURL()))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		var cards []map[string]any
		json.Unmarshal(body, &cards)
		var card map[string]any
		for _, c := range cards {
			if int64(c["id"].(float64)) == int64(id) {
				card = c
				break
			}
		}
		if card == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Card #%d not found", id)), nil
		}

		// Merge provided fields
		if v := mcp.ParseString(req, "title", ""); v != "" {
			card["title"] = v
		}
		if v := mcp.ParseString(req, "description", ""); v != "" {
			card["description"] = v
		}
		if v := mcp.ParseString(req, "column", ""); v != "" {
			card["column"] = v
		}
		if v := mcp.ParseString(req, "project", ""); v != "" {
			card["project"] = v
		}
		if v := mcp.ParseString(req, "pane_target", ""); v != "" {
			card["paneTarget"] = v
		}
		if v := mcp.ParseString(req, "labels", ""); v != "" {
			card["labels"] = v
		}
		if v := mcp.ParseString(req, "card_type", ""); v != "" {
			card["cardType"] = v
		}
		if v := mcp.ParseString(req, "priority", ""); v != "" {
			card["priority"] = v
		}
		// parent_id can be 0 to clear, so check if the key was provided rather than its value
		if req.GetArguments() != nil {
			if _, ok := req.GetArguments()["parent_id"]; ok {
				card["parentId"] = mcp.ParseInt(req, "parent_id", 0)
			}
		}

		// PUT update
		updateBody, _ := json.Marshal(card)
		putReq, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/cards/%d", workshopAPIURL(), id), strings.NewReader(string(updateBody)))
		putReq.Header.Set("Content-Type", "application/json")
		putResp, err := http.DefaultClient.Do(putReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update: %v", err)), nil
		}
		defer putResp.Body.Close()
		if putResp.StatusCode != 200 {
			result, _ := io.ReadAll(putResp.Body)
			return mcp.NewToolResultError(string(result)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Updated card #%d", id)), nil
	}
}

func kanbanAddNoteHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseInt(req, "id", 0)
		text := mcp.ParseString(req, "text", "")
		if id == 0 || text == "" {
			return mcp.NewToolResultError("id and text are required"), nil
		}
		body, _ := json.Marshal(map[string]string{"text": text})
		resp, err := http.Post(fmt.Sprintf("%s/api/v1/cards/%d/notes", workshopAPIURL(), id), "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			result, _ := io.ReadAll(resp.Body)
			return mcp.NewToolResultError(string(result)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Note added to card #%d", id)), nil
	}
}

func kanbanMoveHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseInt(req, "id", 0)
		column := mcp.ParseString(req, "column", "")
		if id == 0 || column == "" {
			return mcp.NewToolResultError("id and column are required"), nil
		}
		body, _ := json.Marshal(map[string]any{"column": column, "position": 0})
		resp, err := http.Post(fmt.Sprintf("%s/api/v1/cards/%d/move", workshopAPIURL(), id), "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			result, _ := io.ReadAll(resp.Body)
			return mcp.NewToolResultError(string(result)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Moved card #%d to %s", id, column)), nil
	}
}

func kanbanDeleteHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseInt(req, "id", 0)
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}
		req2, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/cards/%d", workshopAPIURL(), id), nil)
		resp, err := http.DefaultClient.Do(req2)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		return mcp.NewToolResultText(fmt.Sprintf("Deleted card #%d", id)), nil
	}
}

func consensusStartHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		prompt := mcp.ParseString(req, "prompt", "")
		if prompt == "" {
			return mcp.NewToolResultError("prompt is required"), nil
		}
		dir := mcp.ParseString(req, "directory", "")
		timeout := mcp.ParseInt(req, "timeout", 300)
		agentsStr := mcp.ParseString(req, "agents", "agent1:sonnet,agent2:sonnet,agent3:sonnet")

		// Parse agent specs. Supported formats per agent (comma-separated):
		//   provider                    → e.g. "codex" (auto name, default model)
		//   provider:model              → e.g. "claude:opus", "gemini:pro"
		//   name:provider:model         → e.g. "deep:claude:opus"
		// Provider must be one of: claude, gemini, codex
		// Model defaults: sonnet (claude), pro (gemini), gpt-5.4 (codex)
		validProviders := map[string]bool{"claude": true, "gemini": true, "codex": true}
		defaultModels := map[string]string{"claude": "sonnet", "gemini": "pro", "codex": "gpt-5.4"}
		var agents []map[string]string
		for i, spec := range strings.Split(agentsStr, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			parts := strings.SplitN(spec, ":", 3)
			var name, provider, model string

			switch len(parts) {
			case 1:
				// Single part: must be a provider name
				if validProviders[parts[0]] {
					provider = parts[0]
					name = fmt.Sprintf("agent%d", i+1)
					model = defaultModels[provider]
				} else {
					return mcp.NewToolResultError(fmt.Sprintf("invalid agent spec %q: single value must be a provider (claude/gemini/codex)", spec)), nil
				}
			case 2:
				// provider:model OR name:model (legacy, only if first part isn't a provider)
				if validProviders[parts[0]] {
					provider = parts[0]
					model = parts[1]
					name = fmt.Sprintf("agent%d", i+1)
				} else {
					// Legacy: name:model defaults to claude
					name = parts[0]
					provider = "claude"
					model = parts[1]
				}
			case 3:
				// name:provider:model
				name = parts[0]
				provider = parts[1]
				model = parts[2]
				if !validProviders[provider] {
					return mcp.NewToolResultError(fmt.Sprintf("invalid agent spec %q: provider %q must be claude/gemini/codex", spec, provider)), nil
				}
			}

			agents = append(agents, map[string]string{"name": name, "provider": provider, "model": model})
		}

		if len(agents) == 0 {
			return mcp.NewToolResultError("no valid agents in spec"), nil
		}

		body, _ := json.Marshal(map[string]any{
			"prompt":    prompt,
			"directory": dir,
			"timeout":   timeout,
			"agents":    agents,
		})
		resp, err := http.Post(workshopAPIURL()+"/api/v1/consensus", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 201 {
			return mcp.NewToolResultError(string(result)), nil
		}
		var run map[string]any
		json.Unmarshal(result, &run)
		return mcp.NewToolResultText(fmt.Sprintf("Consensus run started: %s\nStatus: %s\nUse consensus_status(id=\"%s\") to check progress.", run["id"], run["status"], run["id"])), nil
	}
}

func consensusStatusHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/consensus/%s", workshopAPIURL(), id))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}
		var run map[string]any
		json.Unmarshal(body, &run)

		var sb strings.Builder
		fmt.Fprintf(&sb, "Consensus: %s\nStatus: %s\nPrompt: %s\n\n", run["id"], run["status"], run["prompt"])

		if agents, ok := run["agentOutputs"].([]any); ok {
			sb.WriteString("Agents:\n")
			for _, a := range agents {
				agent := a.(map[string]any)
				fmt.Fprintf(&sb, "  %s (%s): %s\n", agent["name"], agent["model"], agent["status"])
			}
		}
		if coord, ok := run["coordinatorPane"].(string); ok && coord != "" {
			fmt.Fprintf(&sb, "\nCoordinator pane: %s\n", coord)
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func consensusListHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := http.Get(workshopAPIURL() + "/api/v1/consensus")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		var runs []map[string]any
		json.Unmarshal(body, &runs)
		if len(runs) == 0 {
			return mcp.NewToolResultText("No consensus runs."), nil
		}

		var sb strings.Builder
		for _, r := range runs {
			fmt.Fprintf(&sb, "=== %s ===\n", r["id"])
			fmt.Fprintf(&sb, "Status: %s\n", r["status"])
			prompt := fmt.Sprintf("%v", r["prompt"])
			if len(prompt) > 100 {
				prompt = prompt[:100] + "..."
			}
			fmt.Fprintf(&sb, "Prompt: %s\n", prompt)
			if agents, ok := r["agentOutputs"].([]any); ok {
				for _, a := range agents {
					agent := a.(map[string]any)
					needs := ""
					if ni, ok := agent["needsInput"].(bool); ok && ni {
						needs = " ⚠️ NEEDS INPUT"
					}
					fmt.Fprintf(&sb, "  %s (%s): %s%s\n", agent["name"], agent["model"], agent["status"], needs)
				}
			}
			sb.WriteString("\n")
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func consensusCaptureHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		agentName := mcp.ParseString(req, "agent", "")
		lines := mcp.ParseInt(req, "lines", 200)
		if id == "" || agentName == "" {
			return mcp.NewToolResultError("id and agent are required"), nil
		}

		// Get the consensus run to find the agent's target
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/consensus/%s", workshopAPIURL(), id))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var run map[string]any
		json.Unmarshal(body, &run)

		// Find the agent target
		var target string
		if agentName == "coordinator" {
			target, _ = run["coordinatorPane"].(string)
		} else if agents, ok := run["agentOutputs"].([]any); ok {
			for _, a := range agents {
				agent := a.(map[string]any)
				if agent["name"] == agentName {
					target, _ = agent["target"].(string)
					break
				}
			}
		}
		if target == "" {
			return mcp.NewToolResultError(fmt.Sprintf("Agent '%s' not found in run %s", agentName, id)), nil
		}

		// Capture the pane output
		session := strings.Split(target, ":")[0]
		captureURL := fmt.Sprintf("%s/api/v1/sessions/%s/capture?target=%s&lines=%d", workshopAPIURL(), session, target, lines)
		captureResp, err := http.Get(captureURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Capture failed: %v", err)), nil
		}
		defer captureResp.Body.Close()
		captureBody, _ := io.ReadAll(captureResp.Body)

		var captureResult map[string]string
		json.Unmarshal(captureBody, &captureResult)
		output := captureResult["output"]

		// Strip ANSI for clean LLM consumption
		clean := stripAnsi(output)
		return mcp.NewToolResultText(fmt.Sprintf("=== %s (%s) ===\n%s", agentName, target, clean)), nil
	}
}

func consensusCleanupHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}
		httpReq, err := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/consensus/%s", workshopAPIURL(), id), nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to build request: %v", err)), nil
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}
		var result map[string]any
		json.Unmarshal(body, &result)
		return mcp.NewToolResultText(fmt.Sprintf("Cleaned up consensus %s: killed %v sessions", id, result["killed"])), nil
	}
}

func consensusReviewHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := mcp.ParseString(req, "id", "")
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		// Get the consensus run
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/consensus/%s", workshopAPIURL(), id))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var run map[string]any
		json.Unmarshal(body, &run)

		var sb strings.Builder
		fmt.Fprintf(&sb, "=== Consensus Review: %s ===\n", id)
		fmt.Fprintf(&sb, "Status: %s\n", run["status"])
		fmt.Fprintf(&sb, "Prompt: %s\n\n", run["prompt"])

		// Capture each agent's output
		if agents, ok := run["agentOutputs"].([]any); ok {
			for _, a := range agents {
				agent := a.(map[string]any)
				name, _ := agent["name"].(string)
				model, _ := agent["model"].(string)
				status, _ := agent["status"].(string)
				target, _ := agent["target"].(string)

				fmt.Fprintf(&sb, "--- Agent: %s (model: %s, status: %s) ---\n", name, model, status)

				if target != "" {
					session := strings.Split(target, ":")[0]
					captureURL := fmt.Sprintf("%s/api/v1/sessions/%s/capture?target=%s&lines=200", workshopAPIURL(), session, target)
					captureResp, err := http.Get(captureURL)
					if err == nil {
						defer captureResp.Body.Close()
						captureBody, _ := io.ReadAll(captureResp.Body)
						var captureResult map[string]string
						json.Unmarshal(captureBody, &captureResult)
						clean := stripAnsi(captureResult["output"])
						sb.WriteString(clean)
					} else {
						sb.WriteString("(capture failed)\n")
					}
				} else {
					sb.WriteString("(no target)\n")
				}
				sb.WriteString("\n\n")
			}
		}

		// Capture coordinator if present
		if coord, ok := run["coordinatorPane"].(string); ok && coord != "" {
			sb.WriteString("--- Coordinator ---\n")
			session := strings.Split(coord, ":")[0]
			captureURL := fmt.Sprintf("%s/api/v1/sessions/%s/capture?target=%s&lines=200", workshopAPIURL(), session, coord)
			captureResp, err := http.Get(captureURL)
			if err == nil {
				defer captureResp.Body.Close()
				captureBody, _ := io.ReadAll(captureResp.Body)
				var captureResult map[string]string
				json.Unmarshal(captureBody, &captureResult)
				clean := stripAnsi(captureResult["output"])
				sb.WriteString(clean)
			}
			sb.WriteString("\n")
		}

		// Auto-cleanup: now that we've captured all outputs, kill the
		// tmux sessions so they don't linger. Only if the run is in a
		// terminal state — we don't want to nuke a still-running consensus.
		status, _ := run["status"].(string)
		if status == "done" || status == "error" || status == "timeout" {
			delReq, err := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/consensus/%s", workshopAPIURL(), id), nil)
			if err == nil {
				if delResp, err := http.DefaultClient.Do(delReq); err == nil {
					delBody, _ := io.ReadAll(delResp.Body)
					delResp.Body.Close()
					var delResult map[string]any
					json.Unmarshal(delBody, &delResult)
					fmt.Fprintf(&sb, "\n(cleaned up %v tmux sessions)\n", delResult["killed"])
				}
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func runConfigHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := mcp.ParseString(req, "path", "")
		code := mcp.ParseString(req, "code", "")
		if path == "" && code == "" {
			return mcp.NewToolResultError("path or code is required"), nil
		}
		body, _ := json.Marshal(map[string]string{"path": path, "code": code})
		resp, err := http.Post(workshopAPIURL()+"/api/v1/config/load", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(result)), nil
		}
		return mcp.NewToolResultText(string(result)), nil
	}
}

func searchOutputHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := mcp.ParseString(req, "query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		target := mcp.ParseString(req, "target", "")
		limit := mcp.ParseInt(req, "limit", 50)

		// Call the Workshop HTTP API
		params := url.Values{"q": {query}, "limit": {fmt.Sprintf("%d", limit)}}
		if target != "" {
			params.Set("target", target)
		}

		resp, err := http.Get(workshopAPIURL() + "/api/v1/search?" + params.Encode())
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop server: %v. Is it running?", err)), nil
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read response: %v", err)), nil
		}

		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(fmt.Sprintf("Search API error: %s", string(body))), nil
		}

		// Pretty-print the results
		var results []map[string]any
		json.Unmarshal(body, &results)
		if len(results) == 0 {
			return mcp.NewToolResultText("No results found."), nil
		}

		var sb strings.Builder
		for _, r := range results {
			fmt.Fprintf(&sb, "[%s line %v] %s\n", r["target"], r["line"], r["content"])
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

// stripAnsi removes ANSI escape sequences from text.
func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// Skip ESC sequence
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
					i++
				}
				if i < len(s) {
					i++ // skip final letter
				}
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

func setPaneStatusHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		status := mcp.ParseString(req, "status", "")
		message := mcp.ParseString(req, "message", "")

		if target == "" || status == "" {
			return mcp.NewToolResultError("target and status are required"), nil
		}

		body, _ := json.Marshal(map[string]string{
			"target":  target,
			"status":  status,
			"message": message,
		})

		resp, err := http.Post(workshopAPIURL()+"/api/v1/panes/status", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop server: %v", err)), nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			return mcp.NewToolResultError(fmt.Sprintf("Error: %s", string(respBody))), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Status set to %s for %s", status, target)), nil
	}
}

func clearPaneStatusHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		target := mcp.ParseString(req, "target", "")
		if target == "" {
			return mcp.NewToolResultError("target is required"), nil
		}

		body, _ := json.Marshal(map[string]string{"target": target})
		httpReq, _ := http.NewRequest("DELETE", workshopAPIURL()+"/api/v1/panes/status", strings.NewReader(string(body)))
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop server: %v", err)), nil
		}
		defer resp.Body.Close()

		return mcp.NewToolResultText(fmt.Sprintf("Status cleared for %s", target)), nil
	}
}

func channelPublishHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"channel": mcp.ParseString(req, "channel", ""),
			"body":    mcp.ParseString(req, "body", ""),
			"from":    mcp.ParseString(req, "from", ""),
			"project": mcp.ParseString(req, "project", ""),
		}
		raw, _ := json.Marshal(body)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/channels/publish", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(out)), nil
		}
		return mcp.NewToolResultText(string(out)), nil
	}
}

func channelSubscribeHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"channel": mcp.ParseString(req, "channel", ""),
			"target":  mcp.ParseString(req, "target", ""),
			"project": mcp.ParseString(req, "project", ""),
		}
		raw, _ := json.Marshal(body)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/channels/subscribe", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Subscribed %s to %s", body["target"], body["channel"])), nil
	}
}

func channelUnsubscribeHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"channel": mcp.ParseString(req, "channel", ""),
			"target":  mcp.ParseString(req, "target", ""),
		}
		raw, _ := json.Marshal(body)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/channels/unsubscribe", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		return mcp.NewToolResultText("unsubscribed"), nil
	}
}

func channelListHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project := mcp.ParseString(req, "project", "")
		u := workshopAPIURL() + "/api/v1/channels"
		if project != "" {
			u += "?project=" + url.QueryEscape(project)
		}
		resp, err := http.Get(u)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return mcp.NewToolResultText(string(out)), nil
	}
}

func channelMessagesHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channel := mcp.ParseString(req, "channel", "")
		limit := mcp.ParseInt(req, "limit", 50)
		if channel == "" {
			return mcp.NewToolResultError("channel is required"), nil
		}
		u := fmt.Sprintf("%s/api/v1/channels/%s/messages?limit=%d", workshopAPIURL(), url.PathEscape(channel), limit)
		resp, err := http.Get(u)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return mcp.NewToolResultText(string(out)), nil
	}
}

// uiActionHandler returns a generic MCP handler that POSTs the named
// UI command to /api/v1/ui/<action> with the requested fields. For
// blocking actions (prompt_user, confirm) it waits for the REST
// handler's response and returns the user's value.
func uiActionHandler(action string, blocking bool, fields []string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload := map[string]any{}
		for _, f := range fields {
			// Try string first; fall back to number/bool by attempting parses.
			if v := mcp.ParseString(req, f, ""); v != "" {
				payload[f] = v
				continue
			}
			if n := mcp.ParseInt(req, f, 0); n != 0 {
				payload[f] = n
				continue
			}
			if b := mcp.ParseBoolean(req, f, false); b {
				payload[f] = b
				continue
			}
		}
		raw, _ := json.Marshal(payload)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/ui/"+action, "application/json", strings.NewReader(string(raw)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop server: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if blocking {
			if resp.StatusCode == 204 {
				return mcp.NewToolResultText("(cancelled by user)"), nil
			}
			if resp.StatusCode != 200 {
				return mcp.NewToolResultError(string(body)), nil
			}
			// Body is {value, cancelled}
			var r struct {
				Value     any  `json:"value"`
				Cancelled bool `json:"cancelled"`
			}
			json.Unmarshal(body, &r)
			if r.Cancelled {
				return mcp.NewToolResultText("(cancelled by user)"), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("%v", r.Value)), nil
		}
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("%s sent", action)), nil
	}
}

// runChannelListener detects this MCP subprocess's pane target and runs a
// long-poll loop against the central Workshop server. When a channel message
// arrives, it emits a notifications/claude/channel notification to the
// parent Claude Code via SendNotificationToAllClients (this MCP subprocess
// only has one client — its parent).
//
// If the pane target can't be detected (e.g. not running inside tmux), the
// listener exits silently and the subprocess falls back to send_text-only
// delivery via the central server's compat mode.
func runChannelListener(s *server.MCPServer) {
	target := detectPaneTarget()
	if target == "" {
		return
	}

	// Connect with retries — the MCP subprocess may start before the workshop
	// HTTP server is ready, especially during full restarts.
	for {
		err := streamChannelListener(s, target)
		if err != nil {
			// Backoff and retry; but if the parent process is going away,
			// the MCP subprocess will be terminated anyway.
			time.Sleep(2 * time.Second)
			continue
		}
		// Clean disconnect — reconnect immediately.
	}
}

// detectPaneTarget reads $TMUX_PANE and resolves it to a session:window.pane target.
func detectPaneTarget() string {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return ""
	}
	// tmux display-message can resolve a pane id (%37) to its target string.
	cmd := exec.Command("tmux", "display-message", "-p", "-t", pane,
		"#{session_name}:#{window_index}.#{pane_index}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func streamChannelListener(s *server.MCPServer, target string) error {
	u := workshopAPIURL() + "/api/v1/channel-listen/" + url.PathEscape(target)
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("listen returned %d", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var event map[string]any
		if err := dec.Decode(&event); err != nil {
			return err
		}
		evType, _ := event["type"].(string)
		switch evType {
		case "ready", "heartbeat":
			continue
		case "message":
			body, _ := event["body"].(string)
			from, _ := event["from"].(string)
			channel, _ := event["channel"].(string)
			project, _ := event["project"].(string)

			// Build the channel notification per Claude Code's spec:
			// https://code.claude.com/docs/en/channels-reference#notification-format
			// content -> body of <channel> tag, meta keys -> tag attributes.
			meta := map[string]string{}
			if from != "" {
				meta["from"] = sanitizeMetaValue(from)
			}
			if channel != "" {
				meta["channel"] = sanitizeMetaValue(channel)
			}
			if project != "" {
				meta["project"] = sanitizeMetaValue(project)
			}
			s.SendNotificationToAllClients("notifications/claude/channel", map[string]any{
				"content": body,
				"meta":    meta,
			})
		}
	}
}

// sanitizeMetaValue replaces characters Claude Code's channel meta parser
// rejects (anything besides letters/digits/underscores in the KEY would
// be dropped, but values are more permissive — keep it simple).
func sanitizeMetaValue(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
}

func openDocHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := mcp.ParseString(req, "path", "")
		if path == "" {
			return mcp.NewToolResultError("path is required"), nil
		}

		body, _ := json.Marshal(map[string]string{"path": path})
		resp, err := http.Post(workshopAPIURL()+"/api/v1/docs/open", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop server: %v", err)), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(fmt.Sprintf("Server returned %d", resp.StatusCode)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Opened %s in Docs view", path)), nil
	}
}

func ollamaModelsHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := http.Get(workshopAPIURL() + "/api/v1/ollama/models")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var models []map[string]any
		json.Unmarshal(body, &models)
		if len(models) == 0 {
			return mcp.NewToolResultText("No models available. Check Ollama endpoints with ollama_health."), nil
		}

		var sb strings.Builder
		for _, m := range models {
			name, _ := m["name"].(string)
			endpoint, _ := m["endpoint"].(string)
			size, _ := m["size"].(float64)
			fmt.Fprintf(&sb, "[%s] %s (%.1f GB)\n", endpoint, name, size/1e9)
		}
		return mcp.NewToolResultText(sb.String()), nil
	}
}

func ollamaChatHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		model := mcp.ParseString(req, "model", "")
		prompt := mcp.ParseString(req, "prompt", "")
		system := mcp.ParseString(req, "system", "")
		think := mcp.ParseBoolean(req, "think", false)

		chatReq := map[string]any{
			"model":    model,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
			"stream":   false,
			"think":    think,
		}
		if system != "" {
			chatReq["messages"] = []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": prompt},
			}
		}
		if temp := mcp.ParseFloat64(req, "temperature", -1); temp >= 0 {
			chatReq["temperature"] = temp
		}
		if maxTok := mcp.ParseFloat64(req, "max_tokens", 0); maxTok > 0 {
			chatReq["max_tokens"] = int(maxTok)
		}

		jsonBody, _ := json.Marshal(chatReq)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/ollama/chat", "application/json", strings.NewReader(string(jsonBody)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var chatResp map[string]any
		json.Unmarshal(body, &chatResp)
		msg, _ := chatResp["message"].(map[string]any)
		content, _ := msg["content"].(string)
		thinking, _ := msg["thinking"].(string)
		if content == "" && thinking != "" {
			content = thinking
		}
		if content == "" {
			return mcp.NewToolResultText("(empty response)"), nil
		}

		// Include stats if available
		var stats string
		var outputTokens float64
		var promptTokens float64
		if evalCount, ok := chatResp["eval_count"].(float64); ok && evalCount > 0 {
			outputTokens = evalCount
			if evalDur, ok := chatResp["eval_duration"].(float64); ok && evalDur > 0 {
				tokPerSec := evalCount / (evalDur / 1e9)
				stats = fmt.Sprintf("\n\n---\n_%.0f tokens, %.1f tok/s, model: %s_", evalCount, tokPerSec, model)
			}
		}
		if pt, ok := chatResp["prompt_eval_count"].(float64); ok {
			promptTokens = pt
		}

		// Record usage (fire-and-forget, don't block on failure)
		if outputTokens > 0 || promptTokens > 0 {
			go postUsage(model, "ollama", int64(promptTokens), int64(outputTokens))
		}

		return mcp.NewToolResultText(content + stats), nil
	}
}

func ollamaGenerateHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		model := mcp.ParseString(req, "model", "")
		prompt := mcp.ParseString(req, "prompt", "")
		system := mcp.ParseString(req, "system", "")
		think := mcp.ParseBoolean(req, "think", false)

		genReq := map[string]any{
			"model":  model,
			"prompt": prompt,
			"stream": false,
			"think":  think,
		}
		if system != "" {
			genReq["system"] = system
		}
		if temp := mcp.ParseFloat64(req, "temperature", -1); temp >= 0 {
			genReq["temperature"] = temp
		}
		if maxTok := mcp.ParseFloat64(req, "max_tokens", 0); maxTok > 0 {
			genReq["max_tokens"] = int(maxTok)
		}

		jsonBody, _ := json.Marshal(genReq)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/ollama/generate", "application/json", strings.NewReader(string(jsonBody)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return mcp.NewToolResultError(string(body)), nil
		}

		var genResp map[string]any
		json.Unmarshal(body, &genResp)
		content, _ := genResp["response"].(string)
		if content == "" {
			return mcp.NewToolResultText("(empty response)"), nil
		}

		var stats string
		var outputTokens float64
		var promptTokens float64
		if evalCount, ok := genResp["eval_count"].(float64); ok && evalCount > 0 {
			outputTokens = evalCount
			if evalDur, ok := genResp["eval_duration"].(float64); ok && evalDur > 0 {
				tokPerSec := evalCount / (evalDur / 1e9)
				stats = fmt.Sprintf("\n\n---\n_%.0f tokens, %.1f tok/s, model: %s_", evalCount, tokPerSec, model)
			}
		}
		if pt, ok := genResp["prompt_eval_count"].(float64); ok {
			promptTokens = pt
		}

		// Record usage (fire-and-forget, don't block on failure)
		if outputTokens > 0 || promptTokens > 0 {
			go postUsage(model, "ollama", int64(promptTokens), int64(outputTokens))
		}

		return mcp.NewToolResultText(content + stats), nil
	}
}

// postUsage fires a POST to the Workshop REST API to record token usage.
// Called as a goroutine — errors are logged but don't block the caller.
func postUsage(model, provider string, inputTokens, outputTokens int64) {
	payload, _ := json.Marshal(map[string]any{
		"sessionId":    mcpPaneTarget,
		"paneTarget":   mcpPaneTarget,
		"provider":     provider,
		"model":        model,
		"inputTokens":  inputTokens,
		"outputTokens": outputTokens,
		"project":      "",
	})
	resp, err := http.Post(workshopAPIURL()+"/api/v1/usage", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		slog.Warn("failed to record ollama usage", "err", err)
		return
	}
	resp.Body.Close()
}

func orchestrateCardHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Prevent recursive calls — if we're already inside an orchestrator session, refuse.
		if strings.HasPrefix(mcpPaneTarget, "orchestrate-") {
			return mcp.NewToolResultError("You ARE the orchestrator. Do not call orchestrate_card. Use launch_agent, capture_pane, report_activity, request_approval, kanban_add_note, and kanban_move directly to drive through the phases."), nil
		}

		cardID := mcp.ParseInt(req, "id", 0)
		if cardID == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}
		directory := mcp.ParseString(req, "directory", "")
		isolation := mcp.ParseString(req, "isolation", "")

		// Fetch the card details
		cardResp, err := http.Get(fmt.Sprintf("%s/api/v1/cards/%d", workshopAPIURL(), cardID))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer cardResp.Body.Close()
		cardBody, _ := io.ReadAll(cardResp.Body)
		if cardResp.StatusCode != 200 {
			return mcp.NewToolResultError(fmt.Sprintf("Card #%d not found", cardID)), nil
		}

		var card map[string]any
		json.Unmarshal(cardBody, &card)
		title, _ := card["title"].(string)
		description, _ := card["description"].(string)
		project, _ := card["project"].(string)

		// Fetch existing notes for context
		notesResp, _ := http.Get(fmt.Sprintf("%s/api/v1/cards/%d/notes", workshopAPIURL(), cardID))
		var notesText string
		if notesResp != nil {
			defer notesResp.Body.Close()
			notesBody, _ := io.ReadAll(notesResp.Body)
			var notes []map[string]any
			json.Unmarshal(notesBody, &notes)
			for _, n := range notes {
				if t, ok := n["text"].(string); ok {
					notesText += "- " + t + "\n"
				}
			}
		}

		// Build the orchestrator prompt — avoid the word "orchestrate" to prevent
		// the agent from recursively calling orchestrate_card on itself.
		prompt := fmt.Sprintf(`Drive card #%d through all workflow phases using the tools described in your system prompt.

## Card Details
**Title:** %s
**Project:** %s
**Description:**
%s
`, cardID, title, project, description)

		if notesText != "" {
			prompt += fmt.Sprintf("\n## Existing Notes\n%s", notesText)
		}

		if directory != "" {
			prompt += fmt.Sprintf("\n## Working Directory\n%s\n", directory)
		}

		prompt += fmt.Sprintf("\nThe card ID is %d and the project is %q. Use these in all kanban_add_note, kanban_move, and report_activity calls.", cardID, project)

		// Launch the orchestrator agent — needs skip-permissions since it
		// operates autonomously (user approves via Activity tab, not CLI prompts)
		launchBody := map[string]any{
			"name":                       fmt.Sprintf("orchestrate-%d", cardID),
			"preset":                     "orchestrator",
			"prompt":                     prompt,
			"directory":                  directory,
			"isolation":                  isolation,
			"background":                 true,
			"dangerouslySkipPermissions": true,
		}
		raw, _ := json.Marshal(launchBody)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/agents/launch", "application/json", strings.NewReader(string(raw)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to launch orchestrator: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 201 {
			return mcp.NewToolResultError(string(result)), nil
		}

		var launched map[string]any
		json.Unmarshal(result, &launched)
		target, _ := launched["target"].(string)
		sessionName, _ := launched["sessionName"].(string)

		msg := fmt.Sprintf("Orchestrator launched for card #%d\nSession: %s\nTarget: %s", cardID, sessionName, target)
		if wt, ok := launched["worktreeDir"].(string); ok && wt != "" {
			br, _ := launched["branch"].(string)
			msg += fmt.Sprintf("\nWorktree: %s\nBranch: %s", wt, br)
		}
		msg += "\n\nThe orchestrator will drive through plan→implement→test→review→PR phases with approval checkpoints at each step. Watch the Activity tab for progress."
		return mcp.NewToolResultText(msg), nil
	}
}

func requestApprovalHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := mcp.ParseString(req, "action", "")
		details := mcp.ParseString(req, "details", "")
		if action == "" || details == "" {
			return mcp.NewToolResultError("action and details are required"), nil
		}
		entry := map[string]any{
			"paneTarget": mcpPaneTarget,
			"agentName":  mcpPaneTarget,
			"action":     action,
			"details":    details,
			"project":    mcp.ParseString(req, "project", ""),
			"diff":       mcp.ParseString(req, "diff", ""),
		}
		body, _ := json.Marshal(entry)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/approvals", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)

		var res map[string]any
		json.Unmarshal(result, &res)
		status, _ := res["status"].(string)

		if status == "approved" {
			return mcp.NewToolResultText("Approved — proceed with: " + action), nil
		}
		reason := ""
		if r, ok := res["reason"].(string); ok {
			reason = " (" + r + ")"
		}
		return mcp.NewToolResultError(fmt.Sprintf("Denied%s — do not proceed with: %s", reason, action)), nil
	}
}

func reportActivityHandler() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := mcp.ParseString(req, "action", "")
		summary := mcp.ParseString(req, "summary", "")
		if action == "" || summary == "" {
			return mcp.NewToolResultError("action and summary are required"), nil
		}
		entry := map[string]any{
			"paneTarget": mcpPaneTarget,
			"agentName":  mcpPaneTarget,
			"actionType": action,
			"summary":    summary,
			"project":    mcp.ParseString(req, "project", ""),
			"metadata":   mcp.ParseString(req, "metadata", "{}"),
			"parentId":   mcp.ParseInt(req, "parent_id", 0),
		}
		body, _ := json.Marshal(entry)
		resp, err := http.Post(workshopAPIURL()+"/api/v1/activity", "application/json", strings.NewReader(string(body)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to reach Workshop: %v", err)), nil
		}
		defer resp.Body.Close()
		result, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 201 {
			return mcp.NewToolResultError(string(result)), nil
		}
		return mcp.NewToolResultText("Activity recorded"), nil
	}
}
