package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/jamesnhan/workshop/internal/tmux"
)

// SendTextDelivery routes channel messages to a target pane by typing
// them into the pane's input via tmux send-keys. This is the "compat"
// mode — works with every Claude Code version, every provider, depends
// on no runtime features. The tradeoff is that the message consumes one
// input "turn" in the receiving session.
//
// Format:
//
//   [channel:<name> from:<sender>] <body>
//
// The receiver sees this as a new user prompt and responds naturally.
type SendTextDelivery struct {
	bridge tmux.Bridge
}

func NewSendTextDelivery(bridge tmux.Bridge) *SendTextDelivery {
	return &SendTextDelivery{bridge: bridge}
}

func (d *SendTextDelivery) Name() string { return "compat/send_text" }

func (d *SendTextDelivery) Deliver(target string, msg ChannelMessage) error {
	// Build a single-line envelope. Collapse internal newlines to spaces
	// because send-keys -l treats \n as Enter/submit, which would split
	// the message prematurely.
	body := strings.ReplaceAll(msg.Body, "\n", " ")
	for strings.Contains(body, "  ") {
		body = strings.ReplaceAll(body, "  ", " ")
	}
	envelope := fmt.Sprintf("[channel:%s from:%s] %s", msg.Channel, msg.From, body)

	// Type the envelope. Long literal text needs time to settle in
	// Claude Code's TUI before Enter is processed.
	if err := d.bridge.SendKeysLiteral(target, envelope); err != nil {
		return fmt.Errorf("send-keys literal: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Verify the text actually landed in the input box. If not, retry.
	for sendAttempt := 0; sendAttempt < 3; sendAttempt++ {
		check, err := d.bridge.CapturePanePlain(target, 5)
		if err == nil && !tmux.IsInputEmpty(check, "claude") {
			break
		}
		time.Sleep(500 * time.Millisecond)
		if err := d.bridge.SendKeysLiteral(target, envelope); err != nil {
			return fmt.Errorf("send-keys literal retry: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Submit. Use the dispatch supervisor's exact pattern: send a single
	// Enter via RunRaw (NOT SendKeys, which appends its own Enter). Verify
	// the input cleared by checking the pane between attempts.
	for attempt := 0; attempt < 3; attempt++ {
		time.Sleep(300 * time.Millisecond)
		if _, err := d.bridge.RunRaw("send-keys", "-t", target, "Enter"); err != nil {
			return fmt.Errorf("send-keys enter: %w", err)
		}
		time.Sleep(1 * time.Second)
		confirmed, err := d.bridge.CapturePanePlain(target, 10)
		if err != nil {
			break
		}
		// If the input is now empty (or showing the working state), it submitted.
		if tmux.IsInputEmpty(confirmed, "claude") {
			break
		}
	}
	return nil
}
