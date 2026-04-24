# Channels (inter-pane messaging)

## Overview
Named pub/sub channels that fan out messages to subscribed panes. Used
primarily for agent-to-agent coordination without having to read and
rewrite terminal output.

## Data model

- `channel_subscriptions(id, channel, target, project, created_at)` — unique
  `(channel, target)`
- `channel_messages(id, channel, sender, body, project, created_at)` —
  persisted history

## API surface

REST:
- `GET /channels?project=` — list active channels with subscriber counts
- `POST /channels/{channel}/publish` body `{body, from?, project?}`
- `POST /channels/{channel}/subscribe` body `{target, project?}`
- `DELETE /channels/{channel}/subscriptions/{target}`
- `GET /channels/{channel}/messages?limit=`
- `GET /channel-listen/{target}` — NDJSON long-poll for native delivery

MCP tools: `channel_publish`, `channel_subscribe`, `channel_unsubscribe`,
`channel_list`, `channel_messages`.

## Delivery modes

1. **Native** — target pane's Claude Code instance registered the
   `claude/channel` experimental capability. `internal/mcp` long-polls
   `/channel-listen/{target}` and emits `notifications/claude/channel` so
   messages arrive as `<channel source="workshop" ...>body</channel>` tags in
   context.
2. **Compat** — fallback: typed into the receiver's input via `send_text`
   as `[channel:X from:Y] body`.
3. **Auto** (default) — picks per-target based on whether the receiver has
   registered a native listener.

## Invariants

1. Publishing to a channel reaches every currently subscribed target exactly
   once per publish.
2. Messages are persisted before fan-out — history survives crashes.
3. Subscription uniqueness: the same `(channel, target)` pair has at most
   one subscription row.
4. Native listener disappearance falls back to compat on the next publish.

## Known edge cases

- **Target renamed / killed**: stale subscriptions should be pruned. Current
  behavior: best-effort cleanup on failed delivery — needs pinning in tests.
- **Large message bodies**: no hard cap yet. Need to decide a sane limit.
- **Publish to channel with zero subscribers**: accepted, stored in history,
  no delivery.

## Test matrix

Legend: ✅ covered, ◻ planned.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Subscribe rejects empty channel | ✅ | | done | ErrEmptyChannelOrTarget |
| 2 | Subscribe rejects empty target | ✅ | | done | |
| 3 | Subscribe is idempotent | ✅ | | done | INSERT OR IGNORE |
| 4 | Unsubscribe removes delivery | ✅ | | done | |
| 5 | Publish rejects empty channel | ✅ | | done | ErrEmptyChannelOrBody |
| 6 | Publish rejects empty body | ✅ | | done | |
| 7 | Publish fans out to all subscribers (compat) | ✅ | | done | |
| 8 | Publish does NOT echo to sender | ✅ | | done | |
| 9 | Publish persists message history | ✅ | | done | |
| 10 | Publish continues on per-target delivery failure | ✅ | | done | |
| 11 | Publish with zero subscribers still persists | ✅ | | done | |
| 12 | Native mode delivers via registered listener | ✅ | | done | |
| 13 | Auto mode falls back to compat with no listener | ✅ | | done | |
| 14 | Auto mode prefers native with active listener | ✅ | | done | |
| 15 | Forced native with no listener skips target | ✅ | | done | |
| 16 | RegisterListener HasListener lifecycle | ✅ | | done | |
| 17 | RegisterListener replaces existing closes old | ✅ | | done | |
| 18 | SetMode toggles at runtime | ✅ | | done | |
| 19 | ListChannels returns subscribed channels | ✅ | | done | |
| 20 | Stale subscription cleanup on target kill | | ◻ | planned | needs pane monitor wiring |
| 21 | Native buffer-full falls back to compat | | ◻ | planned | requires stalled channel fixture |

Backend coverage landed in `internal/server/channels_test.go` (20 tests).
Frontend Settings → Channels configuration roundtrip is planned for a
follow-up.
