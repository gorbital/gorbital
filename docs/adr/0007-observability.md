# ADR-007: Observability

**Status:** Accepted (2026-09-14), amended by ADR-0019 (SDK in its own module), ADR-0028 (Grafana opt-in), ADR-0053 (incoming trace context untrusted unless from trusted callers; HTTP metrics without Host attributes) and ADR-0063 (Prometheus metrics endpoint)

**Context:** Telemetry must be on by default without vendor lock-in or cognitive load.

**Options:** Vendor SDKs; Prometheus + Zap; slog + OpenTelemetry.

**Decision:** slog for logs (stdout), OTel for traces/metrics configured via standard `OTEL_*` env vars; OTLP logs opt-in until the Go logs signal is stable; `/livez`, `/readyz`.

**Why:** Standards with the widest backend support; logs still work with zero infrastructure.

**Tradeoffs:** OTel SDK adds dependency weight and some startup cost.

**Consequences:** All modules instrument via OTel APIs and accept `*slog.Logger`; no module may depend on a vendor telemetry SDK.

## Security review fixes (2026-09-16)

The v1.0 security review (HTTP-1, HTTP-2) found that HTTP telemetry trusted the client:

- **Incoming trace context is untrusted by default.** `HTTPMiddleware` used every request's `traceparent` as the parent span, and the `ParentBased` sampler let its sampled flag override the ratio: a client could hide its requests from tracing (`-00`), force sampling (`-01`), or reuse another request's trace ID, which then reached logs, `audit_events.trace_id` and job metadata. Each request now starts a new root span, sampled by `WithSampleRatio`, with a link to the incoming span context; incoming baggage is never extracted. `telemetry.WithTraceContextFrom(callers)` restores propagation for gateways and internal services, matched on the client address after `httpx.TrustedProxies`; apps set it from `APP_TRUSTED_CALLERS`, which also governs request IDs (ADR-0030).
- **`client.address` is the resolved client.** otelhttp takes it from the first `X-Forwarded-For` value; the middleware now overwrites it with `RemoteAddr`, which `httpx.TrustedProxies` sets only from trusted proxies (ADR-0052).
- **The `Host` header is kept out of metrics.** otelhttp adds `server.address` and `server.port` from `Host` to every HTTP server metric, so about 2000 distinct hosts filled the SDK's cardinality limit and sent later series, 5xx responses included, to the overflow series. `Setup` installs a view that drops both attributes for the otelhttp scope. Spans keep them.

| Check | Result |
|---|---|
| `TestHTTPUntrustedTraceContext` | An unsampled `traceparent` is still traced; a sampled one doesn't force sampling at ratio 0; the span is a new root linked to the incoming trace; the handler sees no baggage; `client.address` is the peer, not `X-Forwarded-For`. Fails without the fix (0 spans exported) |
| `TestHTTPTrustedTraceContextKeepsSampling`, `TestHTTPTraceAndCorrelatedLogs` | A trusted caller's trace, and its sampling decision, carry on; logs carry its trace ID |
| `TestHTTPMetricsIgnoreHost` | 50 `Host` headers give one `http.server.request.duration` series without `server.address` or `server.port` (50 series without the view) |
| Apps | `WithTraceContextFrom(cfg.TrustedCallers)` in all three golden apps; `APP_TRUSTED_CALLERS` validated like `APP_TRUSTED_PROXIES` |
