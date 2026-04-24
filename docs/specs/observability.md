# Observability

## Overview

End-to-end OpenTelemetry across the Workshop stack (Go backend, React frontend,
MCP subprocesses) flowing to a self-hosted LGTM stack running on the user's
Kubernetes cluster.

Workshop's responsibility is generation. The LGTM stack's responsibility is
ingestion, storage, and visualization.

NOT covered here: the LGTM stack's own deployment, scaling, or retention
tuning — that lives in the k8s repo.

## Data flow

```
 ┌────────────────┐   OTLP/HTTP (traces+metrics+logs)
 │ workshop-backend   │ ─────────┐
 │ (Go SDK)       │          │
 └────────────────┘          │
                             │
 ┌────────────────┐          │
 │ workshop-frontend  │ ─────────┤
 │ (Web SDK)      │          │
 └────────────────┘          │
                             ▼
 ┌────────────────┐   ┌────────────────┐     ┌──────┐
 │ workshop-mcp       │ ─▶│ otel-collector │ ─▶  │ LGTM │
 │ (per pane)     │   │ (in k8s)       │     │ stack│
 └────────────────┘   └────────────────┘     └──────┘
                              │                   │
                              │                   ├─ Tempo  (traces)
                              │                   ├─ Mimir  (metrics)
                              │                   ├─ Loki   (logs)
                              │                   └─ Grafana (viz + alerts)
```

- All three Workshop services export **directly** via OTLP/HTTP to a
  collector Service exposed inside the k8s cluster. The local batch span
  processor + retry queue in each SDK handles transient network errors.
- No local OTel Collector on the Workshop host — keeps the moving parts count
  low and means workshop.service has zero observability dependencies at
  startup (export failures degrade gracefully; the app keeps running).
- The in-cluster collector fans out to Tempo / Mimir / Loki via the
  standard OTLP receivers in its pipeline config.

## Endpoint configuration

Every service reads the same env vars:

| Env var | Default | Notes |
|---------|---------|-------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset → SDK disabled | e.g. `https://otel-collector.internal.example:4318` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | prefer HTTP over gRPC for firewall simplicity |
| `OTEL_SERVICE_NAME` | `workshop-backend` / `workshop-frontend` / `workshop-mcp` | set per service |
| `OTEL_SERVICE_VERSION` | git short SHA at build time | injected via `-ldflags` |
| `OTEL_RESOURCE_ATTRIBUTES` | `deployment.environment=dev,host.name=<hostname>` | |
| `WORKSHOP_OTEL_ENABLED` | `false` | master switch — keeps cold local dev zero-cost |
| `WORKSHOP_OTEL_SCRUB_BODIES` | `true` | drop channel/prompt bodies larger than 256 chars |

When `WORKSHOP_OTEL_ENABLED=false` (the default), the SDK initialization
is short-circuited and all instrumentation becomes no-ops. Enable it in
the systemd unit (or via the frontend `VITE_OTEL_ENABLED=true` build
flag) when pointing at a live collector.

## PII / retention policy

Scrubbed at generation time before export:
- **Channel message bodies** over 256 chars (`workshop_channel_publish` spans keep channel name + size only)
- **Agent prompt bodies** over 256 chars (`workshop_agent_launch` spans keep provider + model + prompt size)
- **File paths** inside user home may be replaced with `~/...` if logging outside a span
- **API keys / tokens** — never logged; we don't touch them in Workshop code today

Retention in LGTM is out of scope for this spec — configured in the k8s
repo. Suggested starting point: 14 days traces, 30 days metrics, 14 days
logs.

## Service naming

| Service | `service.name` | Notes |
|---------|----------------|-------|
| Go backend | `workshop-backend` | single binary, REST + WebSocket + channel hub + MCP HTTP endpoints |
| React frontend | `workshop-frontend` | per-browser-tab |
| MCP subprocess | `workshop-mcp` | one per Claude Code pane; tagged with `pane.target` attribute |

## Trace propagation

- Backend uses `otelhttp` on the incoming mux so every REST request gets
  a root span with the W3C traceparent header accepted from clients.
- Frontend `fetch` calls set `traceparent` via
  `@opentelemetry/instrumentation-fetch` so backend spans chain as
  children.
- MCP subprocesses set `traceparent` on outbound HTTP calls to the Workshop
  server so MCP → backend calls appear as single connected traces in
  Tempo.
- WebSocket spans are siblings per message; the connection span is the
  parent.

## Attribute conventions

Where possible, use standard OTel semantic conventions (`http.*`,
`db.*`, `messaging.*`, `code.*`). Workshop-specific attributes use the
`workshop.*` prefix:

- `workshop.pane.target` — tmux pane target (`session:window.pane`)
- `workshop.card.id` — kanban card id
- `workshop.card.project` — project name filter
- `workshop.channel.name` — channel name (always present)
- `workshop.channel.project` — channel project filter
- `workshop.channel.delivery_mode` — `native` / `compat`
- `workshop.agent.provider` — `claude` / `gemini` / `codex`
- `workshop.agent.model` — model string
- `workshop.mcp.tool` — MCP tool name

## Rollout plan

The observability epic is split across tickets #505 → #513:

1. **#505** (this ticket) — decide stack + write this spec ✅
2. **#506** — Go backend SDK bootstrap + HTTP/WS instrumentation
3. **#507** — DB + channel hub tracing
4. **#508** — RED metrics for REST/WS/channels/agents/kanban
5. **#509** — Structured slog logs with trace correlation
6. **#510** — Frontend web SDK instrumentation
7. **#511** — MCP subprocess tracing per tool call
8. **#512** — Grafana dashboards per area (version-controlled under `docs/observability/dashboards/`)
9. **#513** — Alerts

## Test matrix

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | SDK init is no-op when WORKSHOP_OTEL_ENABLED=false | ✅ | | ◻ planned (#506) | |
| 2 | SDK init returns a working shutdown func | ✅ | | ◻ planned (#506) | |
| 3 | HTTP span carries method/path/status | | ✅ | ◻ planned (#506) | |
| 4 | Channel publish span carries channel + mode + delivery count | | ✅ | ◻ planned (#507) | |
| 5 | Body scrubbing respects size threshold | ✅ | | ◻ planned (#506) | |
| 6 | MCP tool span chains with backend span | | ✅ | ◻ planned (#511) | |
| 7 | Frontend fetch sets traceparent | | ✅ | ◻ planned (#510) | |
| 8 | Metrics exporter survives collector unreachable | | ✅ | ◻ planned (#508) | |
| 9 | Shutdown flushes pending spans | | ✅ | ◻ planned (#506) | |

To be populated by each subticket.
