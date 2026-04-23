package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricAttrs is a shorthand for metric.WithAttributes so callers don't
// need to import the metric package directly.
func MetricAttrs(attrs ...attribute.KeyValue) metric.MeasurementOption {
	return metric.WithAttributes(attrs...)
}

// Workshop-specific metrics. Each metric is registered lazily the first time
// InitMetrics is called (from telemetry.Init). When telemetry is disabled,
// the global MeterProvider returns no-op instruments and recording is free.

var (
	meter = otel.Meter("workshop")

	// --- REST ---

	// HTTPRequestsTotal counts HTTP requests. Attributes: method, route, status.
	// (otelhttp already records latency histograms; this counter gives us a
	// cheaper cardinality view for dashboards.)
	HTTPRequestsTotal metric.Int64Counter

	// --- WebSocket ---

	WSConnectionsActive metric.Int64UpDownCounter
	WSMessagesTotal     metric.Int64Counter // direction=in|out, kind=<msg type>

	// --- Channels ---

	ChannelPublishesTotal        metric.Int64Counter // channel, project, mode
	ChannelDeliveryFailuresTotal metric.Int64Counter // channel, mode
	ChannelSubscribersGauge      metric.Int64UpDownCounter

	// --- Kanban ---

	KanbanMutationsTotal metric.Int64Counter // op=create|update|move|delete

	// --- Activity ---

	ActivityEventsTotal   metric.Int64Counter // action_type, project
	ApprovalRequestsTotal metric.Int64Counter // action, decision

	// --- Agents ---

	AgentLaunchesTotal metric.Int64Counter // provider, model

	// --- Usage ---

	AgentTokensTotal metric.Int64Counter   // provider, model, project, direction
	AgentCostUSD     metric.Float64Counter // provider, model, project
)

// InitMetrics registers all metric instruments. Called from Init(); safe
// to call multiple times (instruments are idempotent in the OTel SDK).
func InitMetrics() {
	meter = otel.Meter("workshop")

	// REST
	HTTPRequestsTotal, _ = meter.Int64Counter("workshop_http_requests_total",
		metric.WithDescription("Total HTTP requests"),
	)

	// WebSocket
	WSConnectionsActive, _ = meter.Int64UpDownCounter("workshop_ws_connections_active",
		metric.WithDescription("Currently active WebSocket connections"),
	)
	WSMessagesTotal, _ = meter.Int64Counter("workshop_ws_messages_total",
		metric.WithDescription("Total WebSocket messages"),
	)

	// Channels
	ChannelPublishesTotal, _ = meter.Int64Counter("workshop_channel_publishes_total",
		metric.WithDescription("Total channel publish operations"),
	)
	ChannelDeliveryFailuresTotal, _ = meter.Int64Counter("workshop_channel_delivery_failures_total",
		metric.WithDescription("Total channel delivery failures"),
	)
	ChannelSubscribersGauge, _ = meter.Int64UpDownCounter("workshop_channel_subscribers",
		metric.WithDescription("Current channel subscriber count"),
	)

	// Kanban
	KanbanMutationsTotal, _ = meter.Int64Counter("workshop_kanban_mutations_total",
		metric.WithDescription("Total kanban card mutations"),
	)

	// Activity
	ActivityEventsTotal, _ = meter.Int64Counter("workshop_activity_events_total",
		metric.WithDescription("Total activity log events recorded"),
	)
	ApprovalRequestsTotal, _ = meter.Int64Counter("workshop_approval_requests_total",
		metric.WithDescription("Total approval requests by action and decision"),
	)

	// Agents
	AgentLaunchesTotal, _ = meter.Int64Counter("workshop_agent_launches_total",
		metric.WithDescription("Total agent launches"),
	)

	// Usage
	AgentTokensTotal, _ = meter.Int64Counter("workshop_agent_tokens_total",
		metric.WithDescription("Total agent tokens consumed"),
	)
	AgentCostUSD, _ = meter.Float64Counter("workshop_agent_cost_usd",
		metric.WithDescription("Total agent cost in USD"),
	)
}

func init() {
	// Pre-register with the no-op meter so callers never hit nil pointers
	// even before Init() runs.
	InitMetrics()
}
