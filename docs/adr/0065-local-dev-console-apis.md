# ADR-0065: Local dev console APIs

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0028

## Context

v1.1 adds development-only APIs for a local console (roadmap, feature 10): routes, wiring, configuration without secrets, captured email and recent requests, bound to localhost, checking the `Host` header and a session token printed by `orb dev`. APIs only: the Dev Portal in gorbital-dashboards stays on mock data, and a console UI may be built against these endpoints later. Today:

| Area | Today | Evidence |
|---|---|---|
| Local tools | `orb dev` runs the app, PostgreSQL and Mailpit; Grafana with `--observability`. ADR-0028 lists "Custom dev console: v1.1; may replace Grafana for local viewing" | ADR-0028 |
| Threat model | Row 11, "Localhost services abused from a browser (CSRF, DNS rebinding)": services bound to 127.0.0.1; "custom dev console (v1.1) checks Host header and uses a session token" | ADR-0029 |
| Environment | Apps refuse to start without `APP_ENV`; `orb dev` sets `development` (HTTP-6). Client-controlled request IDs and trace context are trusted only from `APP_TRUSTED_CALLERS`; `X-Forwarded-For` only from `APP_TRUSTED_PROXIES` | ADR-0053, `config.go` |
| Requests | `modules/observability`'s collector sees every request with its route pattern and, for subscribers only, path, request ID and trace ID (`Collector.Subscribe`); Minimal apps have no collector | ADR-0064 |
| Logs | `telemetry.Setup` builds the only logger (`slog`, text or JSON to stdout) and wraps it to add request, trace and organisation IDs; nothing keeps records in memory | `modules/telemetry` |
| Configuration | `LoadConfig` reads every variable through two closures, `get` and `secret` (`config.Secret` never prints); provider files (`infra_mail.go`) receive the same closures | `internal/app/config.go` |
| Wiring data | Settings, flags and job definitions are listed from memory or the database by their stores; permission catalogs by `permissionCatalogs()`; migration state by `postgres.Migrations` | ADR-0031, ADR-0033, ADR-0051, ADR-0057 |
| Middleware | CORS for `APP_CORS_ORIGINS`, cross-origin protection, maintenance mode, authentication and idempotency run for every path in Full apps | `routes.go` |

Constraints: a web page the developer visits must not read or drive the console (CSRF, DNS rebinding); production must never expose it; secrets never leave the process through it; memory is bounded; modules never import each other (ADR-0019).

## Options

### Where the console lives

| Option | Verdict |
|---|---|
| A separate listener on its own port | Rejected: another port to allocate, check and print; a browser still reaches it, so it needs the same Host and token checks; a UI would call two origins |
| Routes in the app's mux behind the normal middleware | Rejected: `APP_CORS_ORIGINS` would add CORS headers, maintenance mode would hide it, the observability collector and access log would record the console's own polling, and authentication would run first |
| **A handler in front of the whole chain for `/_dev/` paths, mounted only in development with a token** | **Chosen**: one port, no application middleware, its own checks first |

### Protecting it from browsers

| Option | Verdict |
|---|---|
| Bind to 127.0.0.1 only | Kept, but not enough: DNS rebinding lets a page on `evil.example` resolve its own name to 127.0.0.1 and read same-origin responses |
| Check `Origin` | Rejected as the only check: requests without `Origin` (same-origin GETs after rebinding, non-browser clients) pass |
| **`Host` must be exactly `localhost`, `127.0.0.1` or `[::1]` with the port the connection arrived on; the peer must be a loopback address; `Authorization: Bearer` with a 256-bit token compared in constant time; no CORS headers** | **Chosen**: rebinding keeps the attacker's host name; a custom header makes every cross-origin request preflighted, and with no CORS answer the browser never sends it; the token stops other local users' tools and pages that guess the port |
| A token in a cookie or query string | Rejected: cookies are sent cross-site, and query strings end up in logs, history and proxies. `EventSource` can't send headers, so streams are read with `fetch` |

### Token lifetime

| Option | Verdict |
|---|---|
| A fixed token in `.env` | Rejected: a secret on disk, copied between machines and into images |
| **`orb dev` generates 256 random bits per run, passes them in the app's environment, prints them once; apps read `DEV_CONSOLE_TOKEN`; a token already in `orb dev`'s own environment is used instead** | **Chosen**: nothing on disk; restarts on file changes keep the token; a UI developer can pin one in their shell |
| Refuse to start in development without a token | Rejected: `go run ./cmd/api` without `orb dev` must keep working; without a token the console doesn't exist |

### Reusable code

| Option | Verdict |
|---|---|
| All in app code | Rejected: the Host, peer and token checks, bounded buffers and streams are security-sensitive and the same in every app; fixes should arrive with `go get` (ADR-0040's argument) |
| Core `httpx` helpers | Rejected: a development tool with response shapes that will change with the UI shouldn't join the stable core API |
| **New `modules/devconsole` (stdlib, core and the OpenTelemetry trace API only), `Stability: stable` Go API (ADR-0015 keeps experimental for `gorbital.dev/x`), with the `/_dev` response shapes not API; one additive `telemetry.WithLogTee` option; apps provide sources** | **Chosen**: Minimal apps use it without PostgreSQL; modules don't import each other (apps adapt `observability.Request`) |

### Requests

| Option | Verdict |
|---|---|
| **Full apps: `Collector.Subscribe`; Minimal apps: `Console.Middleware` with `devconsole.RecordRoute`** | **Chosen**: one request capture per app; Minimal has no collector |
| A second middleware in Full apps | Rejected: duplicates ADR-0064's recorder |

### Configuration listing

| Option | Verdict |
|---|---|
| List `os.Environ()` | Rejected: holds every variable of the shell, including unrelated secrets |
| A hand-written list of keys | Rejected: drifts from `LoadConfig` |
| **Record reads in `LoadConfig`'s `get` and `secret` closures (`devconsole.EnvKeys`): values only for plain reads, set/unset for secrets and for names that look secret (`SECRET`, `PASSWORD`, `TOKEN`, `KEY`, `PRIVATE`, `CREDENTIAL`, `DSN`, `DATABASE_URL`), URL user information and queries redacted** | **Chosen**: exactly the variables the app reads, including provider files, with two layers against a secret read as plain text |
| A hook in core `config.Source` | Rejected: a stable core API change for a development feature; the closures already see every read |

## Decision

### `modules/devconsole`

| Piece | Decision |
|---|---|
| `New(token, options)` | Refuses tokens outside 32–512 visible ASCII characters (`CheckToken`, `ErrInvalidToken`). Options: `WithAddr` (port fallback), `WithMaxRequests` (500), `WithStreams` (8 at once, 30 minutes), `WithLogs`, `WithSources` |
| `Console.Mount(next, logger)` | Paths `/_dev` and `/_dev/…` go to the console, everything else to `next`. A nil console returns `next` |
| Checks, in order | `Host` exactly `localhost`, `127.0.0.1` or `[::1]` (case-insensitive) with the connection's local port (no port only for 80), else 403 `forbidden`; peer address loopback, else 403 `forbidden`; `Authorization: Bearer <token>` compared as SHA-256 hashes with `subtle.ConstantTimeCompare`, else 401 `unauthorized` with `WWW-Authenticate`. Refusals log at most one warning a second |
| Responses | `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`, `X-Frame-Options: DENY`; never CORS headers; GET and HEAD only (405); problem+json errors with existing codes only |
| Endpoints | `/_dev/` (index), `/_dev/openapi.json`, `/_dev/requests` and `/stream`, `/_dev/logs` and `/stream`, and from sources `/_dev/app`, `/_dev/routes`, `/_dev/config`, `/_dev/mail`, `/_dev/migrations`, `/_dev/jobs`. A missing source answers 404 and isn't in the index. Source errors: `ErrUnavailable` → 503 with the message, others → 500 without it (logged) |
| Requests | Ring buffer; `RecordRequest` cuts anything after `?`, bounds path and route to 512 bytes, maps non-standard methods to `_OTHER`; never headers, cookies or bodies. Newest first |
| Logs | `NewLogs(n)` ring buffer; `Logs.Handler()` takes records at info and above whatever `APP_LOG_LEVEL`, resolves `LogValuer`s (secrets print `[redacted]` as in logs), flattens groups, bounds messages (4 KiB), attributes (50, 1 KiB each) and records (8 KiB) |
| Streams | Server-Sent Events `request` or `log`, `dropped` with a count when a client falls 256 events behind, `: keep-alive` every 15 s, final `end` (`max_duration` or `shutdown`); a 10-second write deadline per write; 429 `rate_limited` over the limit; `Close` ends them for shutdown |
| Helpers | `EnvKeys`, `LooksSecret`, `RoutesFromOpenAPI`, `SortRoutes`, `Libraries` (gorbital modules from build information), `MailpitSource` (newest 50 messages from Mailpit's API, 5-second timeout, 4 MiB response limit), response types with JSON tags |
| `OpenAPI()` | An embedded OpenAPI 3.1 document of the console, served at `/_dev/openapi.json`; the app's own document never lists `/_dev/` |
| `telemetry.WithLogTee(h)` | The logger also sends records to `h`, decided by `h`'s own `Enabled`, after request, trace and organisation attributes are added; nil adds nothing |

### Apps

| Piece | Decision |
|---|---|
| Configuration | `DEV_CONSOLE_TOKEN` (secret): in production any value fails `LoadConfig` ("for local development only"); in development it must pass `CheckToken`. Full apps also read `MAILPIT_WEB_PORT` (8025) for `/_dev/mail`. `Config.EnvKeys` holds what `LoadConfig` read |
| Wiring (`internal/app/devconsole.go`) | On only when `APP_ENV=development` and the token is set. `newBase`/`New` create the log buffer and pass `telemetry.WithLogTee`; Full apps subscribe the console to the collector (unsubscribed on close); `buildHTTP` wraps the handler with `Mount`; `Run` closes streams on shutdown |
| All presets | `/_dev/app` (name, version, commit, Go version, env, gorbital libraries, API areas from OpenAPI tags), `/_dev/routes` (OpenAPI operations plus health checks, docs, OpenAPI files and well-known files), `/_dev/config`, requests, logs |
| Full apps | `/_dev/app` adds job definitions, settings with current values, feature flags (targets as counts) and permission catalogs; `/_dev/migrations`; `/_dev/jobs` (50 newest, without arguments); `/_dev/mail` when `MAIL_DELIVERY=mailpit` |
| `orb dev` | When the app's `.env.example` declares `DEV_CONSOLE_TOKEN` and `APP_ENV` is development: 32 random bytes, base64url, in the app process's environment only, the same for reloads during one run; banner `✓ Dev APIs   http://127.0.0.1:<port>/_dev/` and the token. A token in `orb dev`'s environment is used and not printed |

## Why

- The Host check is what defeats DNS rebinding: a rebinding page can change where its name points, not the name the browser sends. Checking the local port too refuses Host values meant for another local service.
- A bearer header forces a CORS preflight the console never answers, so browsers never send token-guessing requests cross-origin, and the token covers the cases the Host check can't (other local processes, pages served from localhost itself).
- Mounting in front of the middleware keeps the app's CORS allow-list, maintenance mode and authentication from changing console behaviour, and keeps the console's own polling out of request counts.
- Recording configuration reads where they happen lists exactly what the app uses, and the name rule is a second barrier if a secret is read as plain configuration.
- Per-run tokens in the process environment leave nothing on disk to leak.

## Trade-offs

- Other processes of the same OS user can read the app's environment (`ps -E`, `/proc/<pid>/environ`) and so the token; the console assumes the developer's user account isn't compromised.
- A console UI on another origin (for example a Next.js app on `localhost:3000`) can't call the APIs from the browser: no CORS. It must proxy them server-side (sending `Host: 127.0.0.1:<port>`) or be served by the app.
- `EventSource` can't send the token; clients read streams with `fetch`.
- Requests and logs are per process and lost on every reload; the console isn't a log store.
- `Libraries` is empty in test binaries, which carry no module build information.
- The handler route list (`handlerRoutes`) is maintained by hand next to `routes.go`; `TestDevConsoleRoutes` checks each listed route is served but can't find unlisted ones.
- Worst-case memory: 500 requests (about 1 KiB each), 1,000 log records (8 KiB each) and 8 streams × 256 queued events: about 25 MiB, typically well under 1 MiB.

## Consequences

- ADR-0028: the "custom dev console" row becomes these APIs; Grafana stays the traces and metrics viewer.
- Threat model row 11: done for the dev console (Host check, loopback peer, per-run bearer token, no CORS, production refusal).
- New public names: env var `DEV_CONSOLE_TOKEN`; `MAILPIT_WEB_PORT` is now also read by Full apps. No error codes, audit actions, permissions, settings or jobs; `/ops` and the apps' OpenAPI documents are unchanged.
- `modules/devconsole` joins the CI lists and CODEOWNERS; `api/modules-devconsole.txt` records its API; `api/modules-telemetry.txt` gains `WithLogTee`.

## Implementation notes (2026-09-16)

- **Deviations from the roadmap and feature notes:** "modules wired" is the linked gorbital library modules (from build information) plus the API's areas (OpenAPI tags), not a list maintained by hand. Streams last 30 minutes (8 at once), longer than `/ops` streams, because a local console stays open. `/_dev/openapi.json` needs the token like every console path. `orb dev` uses a token already in its own environment, so a UI developer can keep one across runs. The peer-address check is an addition to the Host and token checks.
- **Performance** (Apple M1 Max, `go test -bench`): `RecordRequest` 25 ns, no allocations; `Console.Middleware` with `RecordRoute` +290 ns and +3 allocations per request (679 ns against 388 ns for a bare mux, recorder included); a log record through `Logs.Handler` 0.6–1.3 µs and 5 allocations. With the console off, apps pay nothing: no tee, no subscriber (the collector doesn't read paths), `Mount` returns the handler.

| Check | Result |
|---|---|
| `devconsole` `TestAllowedHost`, `TestConsoleRefusesOtherHosts`, `TestConsoleRefusesMissingHost` | `evil.example`, `localhost.evil.example`, `127.0.0.1.nip.io`, `localhost.`, `sub.localhost`, other or missing ports, `[::ffff:127.0.0.1]`, `[0:0:0:0:0:0:0:1]`, `[::]`, `::1` without brackets, `0.0.0.0`, `2130706433`, an HTTP/1.0 request without Host: 403 without console content; app paths unaffected |
| `TestConsoleRequiresToken`, `TestValidTokenComparesHashes`, `TestCheckToken` | Missing, wrong, prefix, longer, schemeless, Basic, token in a list or in the query: 401 with `WWW-Authenticate`; tokens under 32 characters, over 512 or non-ASCII refused |
| `TestConsoleRefusesRemotePeers`, `TestLoopbackPeer` | 192.168.x and 10.x peers refused with a valid Host and token |
| `TestConsoleHeaders` | Success, 401, preflight `OPTIONS`, 404, 405 with `Origin`: no `Access-Control-*`, `Cache-Control: no-store`, `nosniff` |
| `TestRequestsNeverHoldQueriesOrHeaders`, `TestLogsAreBounded`, `TestBuffer` | Query, header, cookie and body values absent; request copies still give the route; 3 of 5 kept; logs: the 5 newest kept, message 4 KiB, record 8 KiB, 50 attributes, `config.Secret` redacted, debug left out |
| `TestEnvKeys`, `TestSourceErrors`, `TestMailpitSource`, `TestRoutesFromOpenAPI`, `TestLibrariesOf` | Secrets and secret-looking names never hold values, URL user info/query/fragment redacted, a secret stays secret; source error text not returned; Mailpit parsing, 503 when down, bad URLs refused |
| `TestStreams`, `TestStreamReportsDroppedEvents` | Events for requests and logs, 429 for a third stream with a limit of 2, `end` on max duration and on `Close`, 503 after `Close`; a slow client gets `dropped` |
| `TestOpenAPIDescribesEveryEndpoint` | Every served path is documented and vice versa; every response property is declared and every declared property served |
| `telemetry` `TestLogTee` | The tee gets info records with request ID, `With` and group attributes while the logger's level is warn; a nil tee changes nothing |
| Apps (all three) `TestDevConsole`, `TestDevConsoleSecurity`, `TestDevConsoleConfigHidesSecrets`, `TestDevConsoleRoutes`, `TestDevConsoleOnlyInDevelopment` | Real listener: requests (and in Full apps a live stream) without secrets, logs, app, routes served, index per preset; rebinding Hosts 403, bad tokens 401, no CORS even for an `APP_CORS_ORIGINS` origin while the app's own CORS works; Full apps with real `DATABASE_URL`, `AUTH_ENCRYPTION_KEYS`, Google and GitHub secrets and the token: none in `/_dev/config`, each listed as set; `/openapi.json` never mentions `_dev`; without a token `/_dev/app` is the app's 404; production with a token and short tokens refused |
| Full apps `TestDevConsoleMail` | 503 with Mailpit down, messages from the repository's Mailpit |
| `cli` `TestDevConsoleTokenPerRun`, `TestDevConsoleTokenOnlyWhenSupported`, `TestDevConsoleTokenFromEnvironment` | 32 random bytes in base64url in the app's environment, printed with the URL, in no file of the app, new per run; none in production, for apps without the variable or with it only in a comment; an environment token used and not printed |
| `TestDevWithDocker` (`ORB_E2E_DOCKER=1`) | Extended, passes for a single-tenant and a multi-tenant Full app on real Docker: the printed token opens `/_dev/app` and `/_dev/mail` (showing the registration email), no token gives 401, `.env` doesn't hold the token |
| `cli` with `ORB_E2E=1` | Pass: generated apps (including `orb add mail` with SMTP and Resend) build and pass their tests with `devconsole_test.go` |
| `TestGoldenAppsDontDrift`, recipe drift, `TestPublicSurface`, `TestOpenAPIUpToDate`, `TestOpsAPICompatible`, apicheck | Pass; API files and `surface.json` unchanged |
