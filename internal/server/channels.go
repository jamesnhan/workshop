package server

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
)

// ChannelHub is a pubsub broker for inter-pane (inter-agent) messaging.
//
// Messages are published to a named channel by any pane. The hub delivers
// the message to every pane currently subscribed to that channel, using
// the configured Delivery strategy.
//
// Two delivery strategies ship today:
//
//   - SendTextDelivery ("compat" mode): types a formatted channel message
//     into the target pane's input via tmux send-keys. Works with every
//     Claude Code version, every provider. Depends on no runtime features.
//
//   - NativeChannelDelivery ("native" mode): emits MCP notifications that
//     satisfy Claude Code's claude/channel capability contract (research
//     preview as of 2026-03). Delivered natively to the target session
//     without polluting its input buffer.
//
// Mode selection is configurable via the delivery_mode field: "compat",
// "native", or "auto" (probe native, fall back to compat per-pane).
type ChannelHub struct {
	db        *db.DB
	logger    *slog.Logger
	mu        sync.RWMutex
	delivery  Delivery
	compat    Delivery // always-available send_text fallback
	mode      DeliveryMode
	listeners map[string]chan ChannelMessage // target → buffered native listener channel
}

// DeliveryMode controls which Delivery strategy the hub uses.
type DeliveryMode string

const (
	DeliveryCompat DeliveryMode = "compat"
	DeliveryNative DeliveryMode = "native"
	DeliveryAuto   DeliveryMode = "auto"
)

// Delivery routes a channel message to a single subscribed pane.
// Implementations include SendTextDelivery (compat) and
// NativeChannelDelivery (claude/channel MCP notifications).
type Delivery interface {
	Name() string
	Deliver(target string, msg ChannelMessage) error
}

// ChannelMessage is the payload delivered to each subscriber.
type ChannelMessage struct {
	ID        int64     `json:"id"`
	Channel   string    `json:"channel"`
	From      string    `json:"from"` // sender pane target or agent name
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"`
	Project   string    `json:"project,omitempty"`
}

// NewChannelHub constructs a hub. The compat delivery is always available
// as a fallback for targets without an active native listener.
func NewChannelHub(database *db.DB, logger *slog.Logger, compat Delivery, mode DeliveryMode) *ChannelHub {
	return &ChannelHub{
		db:        database,
		logger:    logger,
		delivery:  compat,
		compat:    compat,
		mode:      mode,
		listeners: make(map[string]chan ChannelMessage),
	}
}

// SetMode changes the active delivery mode at runtime.
func (h *ChannelHub) SetMode(mode DeliveryMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mode = mode
}

// Mode returns the current delivery mode.
func (h *ChannelHub) Mode() DeliveryMode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.mode
}

// RegisterListener registers a per-target native listener channel. Returns
// a function that unregisters when called. The MCP subprocess for a given
// pane calls this and reads from the returned channel inside its long-poll
// goroutine.
func (h *ChannelHub) RegisterListener(target string) (<-chan ChannelMessage, func()) {
	ch := make(chan ChannelMessage, 16)
	h.mu.Lock()
	// Replace any existing listener for this target — only one MCP
	// subprocess per pane can be active at a time.
	if old, ok := h.listeners[target]; ok {
		close(old)
	}
	h.listeners[target] = ch
	h.mu.Unlock()

	unregister := func() {
		h.mu.Lock()
		if existing, ok := h.listeners[target]; ok && existing == ch {
			delete(h.listeners, target)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, unregister
}

// HasListener reports whether a native listener is currently registered for the target.
func (h *ChannelHub) HasListener(target string) bool {
	h.mu.RLock()
	_, ok := h.listeners[target]
	h.mu.RUnlock()
	return ok
}

// SetDelivery swaps the delivery strategy at runtime (e.g. when the user
// changes the delivery_mode setting).
func (h *ChannelHub) SetDelivery(d Delivery, mode DeliveryMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivery = d
	h.mode = mode
}

// Publish records a message and fans it out to every subscriber of the
// channel. Returns the stored message (with ID + timestamp) and the list
// of target panes that were notified.
func (h *ChannelHub) Publish(channel, from, body, project string) (*db.ChannelMessageRecord, []string, error) {
	channel = strings.TrimSpace(channel)
	body = strings.TrimSpace(body)
	if channel == "" || body == "" {
		return nil, nil, ErrEmptyChannelOrBody
	}

	// Persist the message first so it survives restart and can be queried
	// for history by late subscribers.
	stored, err := h.db.CreateChannelMessage(channel, from, body, project)
	if err != nil {
		return nil, nil, err
	}
	msg := ChannelMessage{
		ID:        stored.ID,
		Channel:   channel,
		From:      from,
		Body:      body,
		Project:   project,
		Timestamp: stored.CreatedAt,
	}

	// Look up subscribers and fan out.
	subs, err := h.db.ListChannelSubscribers(channel)
	if err != nil {
		return stored, nil, err
	}
	var delivered []string
	h.mu.RLock()
	mode := h.mode
	compat := h.compat
	h.mu.RUnlock()

	for _, target := range subs {
		// Don't echo messages back to the sender.
		if target == from {
			continue
		}

		// Choose delivery path. Native is preferred when:
		//   - mode == native (forced), OR
		//   - mode == auto AND a native listener is currently registered.
		// Compat (send_text) is the fallback.
		useNative := false
		switch mode {
		case DeliveryNative:
			useNative = true
		case DeliveryAuto:
			useNative = h.HasListener(target)
		}

		if useNative {
			h.mu.RLock()
			ch := h.listeners[target]
			h.mu.RUnlock()
			if ch != nil {
				select {
				case ch <- msg:
					delivered = append(delivered, target)
					continue
				default:
					// Listener buffer full — fall back to compat below.
					h.logger.Warn("native channel listener buffer full, falling back to compat", "target", target)
				}
			} else if mode == DeliveryNative {
				// Forced native, no listener — record failure and skip.
				h.logger.Warn("native channel delivery requested but no listener", "target", target)
				continue
			}
		}

		if err := compat.Deliver(target, msg); err != nil {
			h.logger.Warn("channel delivery failed", "target", target, "channel", channel, "err", err)
			continue
		}
		delivered = append(delivered, target)
	}
	return stored, delivered, nil
}

// Subscribe registers a pane as a subscriber to a channel. No-op if the
// pane is already subscribed.
func (h *ChannelHub) Subscribe(channel, target, project string) error {
	channel = strings.TrimSpace(channel)
	target = strings.TrimSpace(target)
	if channel == "" || target == "" {
		return ErrEmptyChannelOrTarget
	}
	return h.db.CreateChannelSubscription(channel, target, project)
}

// Unsubscribe removes a pane from a channel.
func (h *ChannelHub) Unsubscribe(channel, target string) error {
	return h.db.DeleteChannelSubscription(channel, target)
}

// ListChannels returns all channels that currently have at least one
// subscriber, optionally filtered by project.
func (h *ChannelHub) ListChannels(project string) ([]db.Channel, error) {
	return h.db.ListChannels(project)
}

// ListMessages returns recent messages on a channel.
func (h *ChannelHub) ListMessages(channel string, limit int) ([]db.ChannelMessageRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	return h.db.ListChannelMessages(channel, limit)
}

// ErrEmptyChannelOrBody is returned by Publish when required fields are missing.
var ErrEmptyChannelOrBody = channelError("channel and body are required")

// ErrEmptyChannelOrTarget is returned by Subscribe when required fields are missing.
var ErrEmptyChannelOrTarget = channelError("channel and target are required")

type channelError string

func (e channelError) Error() string { return string(e) }
