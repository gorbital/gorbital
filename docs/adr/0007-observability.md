# ADR-007: Observability

**Status:** Proposed

**Context:** Telemetry must be on by default without vendor lock-in or cognitive load.

**Options:** Vendor SDKs; Prometheus + Zap; slog + OpenTelemetry.

**Decision:** slog for logs (stdout), OTel for traces/metrics configured via standard `OTEL_*` env vars; OTLP logs opt-in until the Go logs signal is stable; `/livez`, `/readyz`.

**Why:** Standards with the widest backend support; logs still work with zero infrastructure.

**Tradeoffs:** OTel SDK adds dependency weight and some startup cost.

**Consequences:** All modules instrument via OTel APIs and accept `*slog.Logger`; no module may depend on a vendor telemetry SDK.
