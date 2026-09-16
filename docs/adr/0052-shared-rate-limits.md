# ADR-0052: Shared rate limits and trusted proxies

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0019, ADR-0031, ADR-0038

## Context

v1.0 must replace per-instance rate limits with limits shared across instances (roadmap). Today:

| Area | Today | Evidence |
|---|---|---|
| Limiter | Core `ratelimit`: an in-memory token bucket per key (`golang.org/x/time/rate`), `Allow(key) (bool, time.Duration)`, no context, no error; bounded memory, fails open past 100 000 keys | `ratelimit/ratelimit.go` |
| Uses in Full apps | Per-IP limit on `/v1/auth/*` (60 a minute, `routes.go`); per-address sign-in limit (10 in 15 minutes, second factors included); per-user limit on 2FA changes; "account exists" notices (one a minute per address) | `internal/app/routes.go`, `internal/modules/auth/usecase/{login,login_mfa,mfa,register}.go` |
| Instances | Each keeps its own buckets, so N instances allow N times each limit; a load balancer spreading requests multiplies brute-force budgets | ADR-0038 trade-off |
| Client IP | `ratelimit.ByRemoteIP` and `auth.ClientInfoFrom` use `RemoteAddr`. No trusted-proxy handling exists: behind a load balancer every request has the balancer's address | `httpx`, comment in `routes.go` |
| Limits | Code constants (`auth.DefaultLoginAttempts`, `authRequestsPerMinute`), not runtime settings | `modules/auth/auth.go` |

Constraints: PostgreSQL is the only required service (ADR-0014); core can't depend on pgx (ADR-0019); generated apps own their wiring and receive changes through `orb upgrade` (ADR-0050); tunables are runtime settings, secrets and infrastructure are environment variables (ADR-0031).

The maintainer decided (2026-09-15): when the database can't answer, fall back to per-instance limits; include trusted-proxy handling; make the sign-in limits runtime settings.

**Why trusted proxies belong here:** a shared per-IP limit behind a load balancer keys every user by the balancer's IP, so one person's retries would lock out everyone. Shared limits are unsafe without it.

## Options

| Option | Verdict |
|---|---|
| Redis or Valkey | Rejected: a second required service |
| Divide each limit by the number of instances | Rejected: wrong under autoscaling and uneven load balancing |
| Sticky sessions | Rejected: attackers choose their connections |
| Fixed-window counters in PostgreSQL | Rejected: allow twice the limit across a window boundary |
| Sliding log (a row per request) | Rejected: a write and a growing table per request |
| **GCRA in PostgreSQL: one row per key, one statement per decision** | **Chosen**: exact token-bucket semantics (rate and burst, as today), constant storage per key, atomic across instances |

## Decision

### 1. Core `ratelimit`: a small interface

```go
// Decision is the outcome of one request against a limit.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration // when not allowed
}

// A Taker decides whether a request for key may proceed. An error means the
// limiter couldn't decide; callers allow the request.
type Taker interface {
	Take(ctx context.Context, key string) (Decision, error)
}
```

- The in-memory `*Limiter` gains `Take`; `Allow` stays for compatibility and is documented as the in-memory shortcut.
- `Middleware` takes a `Taker` (a `*Limiter` still fits, so existing calls compile). An error allows the request: a store that can fail handles its own fallback (below).
- Core stays within its budget: no new dependency.

### 2. `modules/ratelimitpg`: the shared limiter

A new module, like `auditpg`: core `ratelimit`, `modules/postgres` and pgx.

```go
l, err := ratelimitpg.New(pool, "auth_login", ratelimit.Limit{PerSecond: 10.0 / 900, Burst: 10},
	ratelimitpg.WithLogger(logger))
d, err := l.Take(ctx, "ada@example.com")
```

| Topic | Decision |
|---|---|
| Algorithm | GCRA (generic cell rate algorithm): each key stores its theoretical arrival time `tat`. With emission interval `T = 1/rate` and tolerance `T × burst`, a request at `now` is allowed when `max(tat, now) + T − now ≤ T × burst`, and then `tat` becomes `max(tat, now) + T`; otherwise `RetryAfter = max(tat, now) + T − now − T × burst`. Same decisions as a token bucket with that rate and burst |
| Storage | `ratelimit_buckets (key bytea PRIMARY KEY, tat timestamptz, expires_at timestamptz)`, **UNLOGGED** (no WAL; a crash resets budgets, which is acceptable for limits). Keys are SHA-256 of the limiter name and key, so no email address or IP is stored |
| Statement | One `INSERT … ON CONFLICT (key) DO UPDATE … WHERE <allowed> RETURNING tat` decides and records atomically; a denied request reads `tat` for `Retry-After`. Row locks serialise only requests for the same key |
| Time | The database clock (`statement_timestamp()`), so instances with skewed clocks agree; `WithClock` for tests |
| Limit changes | `Limit` is read on every call from a function, so runtime settings apply at once |
| Cleanup | `DeleteExpired(ctx, limit)`: rows whose `expires_at` passed (a full bucket again). The app runs it hourly in a `ratelimit_cleanup` job |
| Migrations | Embedded in the module as `ratelimitpg.Migrations` and copied into the app's `db/migrations`, like `auditpg` |
| Deadline | Each decision has its own 250 ms timeout, so a slow database can't hold requests |

### 3. Failing safe: per-instance fallback, and a local pre-check

- **Fallback (maintainer's decision):** when the statement fails or times out, `Take` decides with an in-memory `ratelimit.Limiter` of the same limit, logs a warning at most once a minute per limiter, and counts `ratelimit.fallbacks`. Sign-in keeps working with today's per-instance protection during an outage.
- **Local pre-check:** before the database, each limiter checks an in-memory bucket with twice the burst. A client far over its limit is refused without a database write, so a flood of requests can't become a flood of writes. Normal clients never reach the local limit first.
- `Take` returns an error only for invalid use (an empty key); outages never surface as errors.

### 4. Trusted proxies

| Topic | Decision |
|---|---|
| Configuration | `APP_TRUSTED_PROXIES`: comma-separated CIDRs of load balancers and reverse proxies, such as `10.0.0.0/8,172.16.0.0/12`. Empty (the default) trusts no header. Infrastructure, so an environment variable (ADR-0031) |
| Middleware | `httpx.TrustedProxies(prefixes)`: when `RemoteAddr` is in a trusted range, walk `X-Forwarded-For` from the right, skip trusted addresses, and use the first untrusted one as the client, replacing `r.RemoteAddr`. A request from an untrusted address keeps its own address and its headers are ignored, so clients can't spoof their IP |
| Placement | First in the chain after recovery, so access logs, audit client info and every limiter see the client's address |
| Validation | Invalid CIDRs stop the app at start; `0.0.0.0/0` and `::/0` are refused (they would trust every client's header) |
| Not included | `Forwarded` (RFC 7239) and PROXY protocol: added when a supported platform needs them |

### 5. Limits as runtime settings

| Setting | Default | Bounds | Used by |
|---|---|---|---|
| `auth.ip_requests_per_minute` | 60 | 10–10 000 | Per-IP limit on `/v1/auth/*` |
| `auth.login_attempts` | 10 | 3–100 | Per address, sign-in and second factors |
| `auth.login_window` | 15 min | 1 min–24 h | With `auth.login_attempts` |
| `auth.mfa_change_attempts` | 10 per 15 min | 3–100 | Per user, 2FA changes |

"Account exists" notices stay at one a minute: an anti-abuse rule for email, not a tunable. Settings are audited and apply to every instance through LISTEN/NOTIFY (ADR-0031).

### 6. Wiring

- **Full presets:** every limiter above uses `ratelimitpg`; the auth use cases take a `ratelimit.Taker` per limit instead of building their own; the `ratelimit_cleanup` job is defined; `APP_TRUSTED_PROXIES` is read and the middleware installed; the four settings are declared.
- **Minimal preset:** in-memory limiters and trusted proxies (no database).
- `orb upgrade` adds the module, migration, job, setting declarations and wiring; `.env.example` documents `APP_TRUSTED_PROXIES`.

### Observability

- Spans from the pool's tracer on each decision (`db.statement` names the operation only).
- Counters in `ratelimitpg` through the OpenTelemetry metric API: `ratelimit.decisions` (attributes `limiter`, `allowed`), `ratelimit.fallbacks` (`limiter`, `reason`).
- `GET /ops/system` shows fallbacks since start per limiter.
- A denied request is logged at debug; audit keeps recording `auth.login.failed` with reason `rate_limited` as today.

### Testing

| Level | Checks |
|---|---|
| Unit (`ratelimit`) | `Take` matches `Allow`; `Middleware` with a `Taker`, errors allowed |
| Property (`ratelimitpg`) | For random request times, rates and bursts, decisions and `RetryAfter` match the in-memory token bucket |
| Integration (Docker PostgreSQL) | Three limiters on one pool (three "instances") hammered concurrently on one key allow exactly the burst; limit changes apply to the next call; expired rows deleted; keys stored hashed; database stopped or statement timeout forces the fallback and the warning; the local pre-check refuses a flood without writes |
| Middleware (`httpx`) | Trusted and untrusted peers, multiple proxies, spoofed headers from untrusted peers, IPv6, invalid and all-trusting CIDRs |
| App end to end | Two app instances on one database share the sign-in limit; behind a trusted proxy, two clients get separate per-IP budgets; settings change limits live |
| Benchmark | `Take` against local PostgreSQL, allowed and denied paths |

## Why

- GCRA keeps today's rate-and-burst behaviour exactly, with one row and one statement per decision.
- PostgreSQL is already required; an unlogged table makes it cheap enough for authentication paths.
- Falling back to per-instance limits keeps sign-in available and never removes protection entirely.
- The local pre-check stops the limiter from becoming a way to load the database.
- Trusted proxies make per-IP limits and audit addresses correct behind load balancers.

## Trade-offs

- A database round trip on every rate-limited request (authentication paths only), and a hot key serialises its own requests.
- A crash or failover resets budgets (unlogged table).
- During a database outage, limits are per instance again.
- Operators must list their proxies; a wrong `APP_TRUSTED_PROXIES` either ignores real client IPs or trusts spoofed ones (documented, and all-trusting ranges refused).
- One more module, table, job and four settings.

## Consequences

- `ratelimit` gains `Taker`, `Decision`, `Limit` and `(*Limiter).Take` (additions only); `Middleware` accepts a `Taker`.
- New module `modules/ratelimitpg` (pre-1.0, ADR-0015); `httpx.TrustedProxies`.
- Full apps: changed auth use-case config (limiters injected), new settings, job, migration and middleware; upgrade notes describe the behaviour change: **limits now apply across all instances**, so multi-instance deployments see stricter effective limits.
- Threat model: rate-limit rows (credential stuffing, 2FA guessing) gain shared enforcement; a new row for client IP spoofing through forwarded headers.
- ADR-0038's "rate limits are per instance" trade-off and "trusted-proxy client IP handling" follow-up are resolved.

## Implementation notes (2026-09-15)

- **Core:** `ratelimit` gains `Limit` (with `Per(n, window)` and `Valid`), `Decision`, `Taker`, `ErrEmptyKey` and `(*Limiter).Take`; `Middleware` takes a `Taker`, so existing calls with a `*Limiter` compile unchanged. `httpx` gains `ParseTrustedProxies` (CIDRs or single addresses; `/0` refused with `ErrTrustAll`) and `TrustedProxies`. No new core dependency.
- **`modules/ratelimitpg`:** `NewStore(pool, opts)` and `store.Limiter(name, limit func(ctx) ratelimit.Limit)`, so a limit backed by runtime settings applies to the next request. The in-memory pre-check and fallback are rebuilt when the limit changes.
- **Storage differs from the decision above in one detail:** the table has no `expires_at`. A bucket is full again exactly when `tat` has passed, so `tat` serves both the decision and `DeleteExpired` (indexed). One statement decides, and returns the snapshot's previous `tat` and the clock for the retry time, so a refused request costs one round trip too.
- **Deferred:** fallback counts in `GET /ops/system`. The OpenTelemetry counters `ratelimit.decisions` (`limiter`, `allowed`, `source` = database, local or fallback) and `ratelimit.fallbacks` (`limiter`, `reason` = timeout or error) and a warning at most once a minute per limiter cover operations for now.
- **Apps:** `internal/app/rate_limits.go` builds four limiters (`auth_ip`, `auth_login`, `auth_mfa`, `auth_notice`) on the app's pool; the auth use cases take `LoginLimiter`, `MFALimiter` and `NoticeLimiter` (in-memory defaults when nil, for tests and other wiring); the per-IP middleware uses the shared limiter; `APP_TRUSTED_PROXIES` in all three golden apps; settings `auth.ip_requests_per_minute`, `auth.login_attempts`, `auth.login_window` and `auth.mfa_change_attempts` in group `rate_limits`, reason required; the hourly `ratelimit_cleanup` job; the module's migration copied as `20260917000002_ratelimit_buckets.sql`. The 2FA-change limit now has its own budget instead of sharing the sign-in limiter under an `mfa:` prefix.
- **Performance:** `BenchmarkTake` against the Docker PostgreSQL on a laptop: about 0.43 ms per decision, including the round trip.

| Check | Result |
|---|---|
| Model test (`TestTakeMatchesModel`) | 25 random limits × 60 random request times: every decision and retry time equals a reference GCRA in integer microseconds |
| Concurrency (`TestSharedAcrossInstances`, race detector) | 45 concurrent requests through three limiters on one key allow exactly the burst of 5; another limiter name keeps its own budget |
| Limits, keys, cleanup | A changed limit applies to the next request; no row contains the raw key; `DeleteExpired` removes a bucket only once it is full again |
| Failure | With the pool closed, decisions keep the burst in memory and log one warning; requests past twice the burst are refused locally without touching the database |
| Trusted proxies | Direct clients, spoofed headers from untrusted peers, one and several proxies, client-supplied hops left of the proxy, IPv6 and IPv4-mapped peers, malformed hops, all-trusting ranges |
| Apps | Two instances on one database share the sign-in limit (`TestSignInLimitSharedAcrossInstances`); behind a trusted proxy two clients get separate per-IP budgets while an untrusted peer claiming new addresses is still limited (`TestPerIPLimitBehindTrustedProxy`); `APP_TRUSTED_PROXIES` validation |

## Security review fixes (2026-09-16)

- **HTTP-8:** `docs/guides/production.md` still told operators to add their own `X-Forwarded-For` middleware and that limits were per instance. It now points to `APP_TRUSTED_PROXIES` and the shared limits; a hand-written middleware trusting every peer would have reopened client IP spoofing. The comment on `authLimitKey` in the Full apps' `routes.go` said the same and was corrected.
- **HTTP-4:** `APP_CORS_ORIGINS` entries are trusted by cross-origin protection and as sign-in `return_to` origins, but http origins were accepted in production. `LoadConfig` now refuses non-https origins when `APP_ENV=production`, like `WEBAUTHN_ORIGINS`; `httpx.CORS` parses each origin and refuses user info, paths, queries and fragments (`https://a@evil.example` passed the old prefix check).
- **Trusted proxies and correlation:** trace context and request IDs aren't trusted from `APP_TRUSTED_PROXIES`, because load balancers usually pass clients' `traceparent` and `X-Request-ID` through unchanged. They have their own list, `APP_TRUSTED_CALLERS`, matched on the client address this middleware resolves (ADR-0007, ADR-0030).

| Check | Result |
|---|---|
| Apps `TestLoadConfigSecureDefaults` | `https://` origins pass in production and `http://localhost:3000` in development; an `http://` origin in production fails naming `APP_CORS_ORIGINS`; `APP_TRUSTED_CALLERS=0.0.0.0/0` is refused |
| `httpx` `TestCORS` | Origins with user info, path, query, fragment, another scheme or no host are refused; IPv6 and port origins pass |
