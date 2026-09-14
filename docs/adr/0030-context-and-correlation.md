# ADR-0030: Context and correlation propagation

**Status:** Accepted (2026-09-14)

## Context

Requests, background jobs, audit events, logs and traces must be linkable: who acted, in which organisation, from which request. Misusing `context.Context` (loggers, transactions or optional parameters in context) hides dependencies. Work triggered by a request often continues in a job after the request ends, and some writes must survive client disconnects.

## Options

1. Pass everything explicitly through parameters.
2. Put everything in context (logger, transaction, config).
3. Context only for request-scoped identity and correlation; everything else explicit.

## Decision

Option 3.

| Data | Where it lives |
|---|---|
| Actor (user or system, platform permissions) | Context via core `actor` |
| Tenant (`org_id`) in multi-tenant apps | Context, set by `orgs.RequireMember` after authorisation |
| Request ID | Context; read from or written to `X-Request-ID` |
| Trace and span | Context (OpenTelemetry) |
| Logger | **Not in context.** `*slog.Logger` injected via constructors; call `InfoContext(ctx, …)`; a handler wrapper adds request ID, trace ID, span ID and org ID from context |
| Database transaction | **Not in context.** Passed explicitly through `TxManager` and tx-bound repositories |
| Configuration | **Not in context.** Constructors and options (ADR-0020) |

### Rules

- `ctx` is the first parameter of every function that does I/O or may block; never stored in structs.
- **Jobs:** at enqueue, job metadata stores request ID, trace parent, actor and org ID; the worker restores them into context, with the actor marked as `system` acting on behalf of the original actor.
- **Detached writes:** audit records for failed or denied actions and security-critical writes (session revocation) use `context.WithoutCancel(ctx)` with their own timeout.
- **Outgoing calls:** derived contexts with timeouts; always `defer cancel()`.
- **Libraries** never call `slog.Default()` or create root contexts, except `main` and the lifecycle helper.
- **Log attributes:** snake_case keys; log `user_id` and `org_id`, never emails, tokens or secrets.

## Why

Everything stays linkable without hiding real dependencies in context.

## Trade-offs

Jobs need a small amount of metadata handling (provided by the jobs module).

## Consequences

The e2e test suite asserts that a request's `request_id` appears in its log lines, audit event and any jobs it enqueued.
