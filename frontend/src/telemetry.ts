/**
 * Frontend OpenTelemetry bootstrap.
 *
 * When VITE_OTEL_ENABLED is "true" at build time, this module initialises
 * a WebTracerProvider that:
 *   - Auto-instruments fetch calls (adds traceparent header so backend
 *     spans chain as children of the browser click/route)
 *   - Exports via OTLP/HTTP to VITE_OTEL_ENDPOINT (defaults to the
 *     same host, path /v1/traces — the collector ingress)
 *   - Identifies the service as "workshop-frontend"
 *
 * When the flag is absent or false, this module is a no-op and the
 * instrumentation libs are tree-shaken by Vite.
 */

const ENABLED = import.meta.env.VITE_OTEL_ENABLED === 'true';

export function initTelemetry() {
  if (!ENABLED) return;

  // Dynamic imports so the SDK is only loaded (and bundled) when enabled.
  Promise.all([
    import('@opentelemetry/sdk-trace-web'),
    import('@opentelemetry/sdk-trace-web').then((m) => m), // also exports BatchSpanProcessor via sdk-trace-base re-export
    import('@opentelemetry/exporter-trace-otlp-http'),
    import('@opentelemetry/instrumentation-fetch'),
    import('@opentelemetry/context-zone'),
    import('@opentelemetry/resources'),
    import('@opentelemetry/semantic-conventions'),
    import('@opentelemetry/instrumentation'),
  ]).then(([
    { WebTracerProvider },
    { BatchSpanProcessor },
    { OTLPTraceExporter },
    { FetchInstrumentation },
    { ZoneContextManager },
    { resourceFromAttributes },
    { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION },
    { registerInstrumentations },
  ]) => {
    const endpoint =
      import.meta.env.VITE_OTEL_ENDPOINT ||
      `${window.location.protocol}//${window.location.host}/v1/traces`;

    const resource = resourceFromAttributes({
      [ATTR_SERVICE_NAME]: 'workshop-frontend',
      [ATTR_SERVICE_VERSION]: import.meta.env.VITE_APP_VERSION || 'dev',
    });

    const exporter = new OTLPTraceExporter({ url: endpoint });

    const provider = new WebTracerProvider({
      resource,
      spanProcessors: [new BatchSpanProcessor(exporter)],
    });

    // ZoneContextManager preserves async context across microtasks so
    // child spans created inside fetch callbacks chain correctly.
    provider.register({ contextManager: new ZoneContextManager() });

    // Auto-instrument all fetch calls — injects traceparent header.
    registerInstrumentations({
      instrumentations: [
        new FetchInstrumentation({
          // Only propagate to our own backend, not to external APIs.
          propagateTraceHeaderCorsUrls: [/\/api\/v1\//],
        }),
      ],
    });

    // eslint-disable-next-line no-console
    console.log('[workshop-telemetry] frontend tracing enabled →', endpoint);
  }).catch((err) => {
    // eslint-disable-next-line no-console
    console.warn('[workshop-telemetry] failed to initialise:', err);
  });
}
