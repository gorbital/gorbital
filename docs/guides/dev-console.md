# Dev console APIs

While you run an app with `orb dev`, it serves development-only JSON APIs under `/_dev/`: what the app wired, its routes, the environment variables it read (secrets only as set or unset), recent requests and log records with live streams, email previews, migration state and recent job runs. They are for local tools, such as a console UI, scripts or an editor extension. Decision: [ADR-0065](../adr/0065-local-dev-console-apis.md). Library: `gorbital.dev/modules/devconsole`.

The APIs never exist in production, and nothing about them appears in the app's OpenAPI document. They have their own small OpenAPI document at `/_dev/openapi.json`.

## Turning them on

`orb dev` does it. On every run it generates a random token (256 bits), gives it to the app in its environment as `DEV_CONSOLE_TOKEN`, and prints it once:

```text
  ✓ API        http://127.0.0.1:8080
  ✓ API docs   http://127.0.0.1:8080/docs
  ✓ Emails     http://127.0.0.1:3100/mail (caught at 127.0.0.1:1025)
  ✓ Dev APIs   http://127.0.0.1:8080/_dev/ (docs/guides/dev-console.md)
    Token      q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E (Authorization: Bearer; new on every orb dev run)
```

The token is never written to `.env` or any other file, and it stays the same while `orb dev` rebuilds and restarts the app. The next `orb dev` run prints a new one.

| Situation | What happens |
|---|---|
| `APP_ENV=development` and `DEV_CONSOLE_TOKEN` set | The APIs are on |
| `DEV_CONSOLE_TOKEN` empty | They don't exist: `/_dev/app` is the app's ordinary 404 |
| `APP_ENV=production` with `DEV_CONSOLE_TOKEN` set | The app refuses to start: `DEV_CONSOLE_TOKEN is for local development only: unset it in production` |
| Token shorter than 32 characters | The app refuses to start |
| `DEV_CONSOLE_TOKEN` already set in the shell that runs `orb dev` | `orb dev` passes that token instead of generating one, and doesn't print it: useful when a tool you're building needs the same token across runs |
| `go run ./cmd/api` without `orb dev` | Off, unless you export a token yourself |

## Calling them

```bash
TOKEN=q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/_dev/
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/_dev/requests
curl -N -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/_dev/logs/stream
```

Every request must pass three checks, in this order:

| Check | Refused with |
|---|---|
| The `Host` header is exactly `localhost`, `127.0.0.1` or `[::1]` with the port the app listens on | 403 `forbidden` |
| The connection comes from a loopback address (so an app listening on `0.0.0.0` doesn't serve the console to your network) | 403 `forbidden` |
| The request carries no forwarding headers (`Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, `X-Real-IP`, `True-Client-IP`, `CF-Connecting-IP`, `CF-Ray`, `CDN-Loop`): a tunnel or proxy on your machine connects from loopback, so this is what keeps [`orb dev --tunnel`](../dev-portal/tunnel.md) from opening the console to the internet ([ADR-0086](../adr/0086-dev-portal-tunnel.md)) | 403 `forbidden` |
| `Authorization: Bearer <token>` holds the token | 401 `unauthorized` |

Responses are JSON (errors are problem+json like the rest of the API), say `Cache-Control: no-store`, and never carry CORS headers. Only GET (and HEAD) is accepted. The token is never accepted in a query string or cookie.

### Acting on `/ops/` with the token

In Full apps the token also opens the [operations APIs](ops-api.md) in development ([ADR-0066](../adr/0066-dev-portal.md)): a request under `/ops/` with `Authorization: Bearer <token>` that passes the same Host, loopback and forwarding checks runs as the system actor `dev-console` with the platform administrator's permissions, and audit events record it that way. The Dev Portal uses this through `orb dev`'s proxy; scripts can too:

```bash
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/jobs/definitions
curl -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/jobs/definitions/heartbeat/run
```

Nothing outside `/ops/` accepts the token, a cookie never carries it, and without the console (no token, or production, which refuses it) `/ops/` needs a signed-in administrator as always.

### Why these checks

A page on any website you visit can make your browser send requests to `http://127.0.0.1:8080`. Without CORS headers it can't read the answers, but with **DNS rebinding** it can: the attacker's name `evil.example` first resolves to their server, then to `127.0.0.1`, so the page is "same-origin" with your app. The browser still sends `Host: evil.example:8080`, which the console refuses. The bearer token covers what the Host check can't: other programs on your machine, and pages served from localhost itself. And because the token travels in a header, a cross-origin page can't even send a request with it: the browser asks first (CORS preflight), and the console never says yes.

Other programs running as your own user can read a process's environment, and so the token. The console assumes your user account isn't compromised.

## Building a UI on them

The [Dev Portal](dev-portal.md) is the UI built on these APIs: `orb dev` serves it on port 3100 and proxies `/_portal/app/_dev/…` to the app with the token added, so the browser never holds it ([ADR-0066](../adr/0066-dev-portal.md)). For a UI of your own:

- **Serve it from the app's origin, or proxy.** Browsers on another origin (such as a Next.js app on `http://localhost:3000`) can't call the APIs directly: there is no CORS. Call them from your UI's server (route handlers, rewrites or a proxy), sending the token and a `Host` of `127.0.0.1:<port>` (what `fetch` to `http://127.0.0.1:8080` sends). Keep the token on the server side.
- **Read streams with `fetch`.** `EventSource` can't send an `Authorization` header. Read `text/event-stream` from `fetch`'s body instead.
- **Take the shapes from `/_dev/openapi.json`.** It describes every endpoint and response; generate types from it. Fields may be added (the module is `Stability: experimental`); changes are listed in the upgrade notes.
- **Expect reloads.** `orb dev` restarts the app on every change; recent requests and logs start empty again, and streams end (reconnect).

## Endpoints

| Endpoint | Presets | Returns |
|---|---|---|
| `GET /_dev/` | All | `{"endpoints": [...], "extensions": [...]}`: the paths this app serves, and the prefixes extensions serve |
| `GET /_dev/openapi.json` | All | The console's OpenAPI 3.1 document |
| `GET /_dev/app` | All | Name, version, commit, Go version, `env`, linked gorbital `libraries`, API `modules` (OpenAPI tags); Full apps also `jobs` (definitions with schedule and next run), `settings` (current and default values), `flags` (state, with allow and deny lists as a count) and `permissions` (catalogs with roles) |
| `GET /_dev/routes` | All | Every OpenAPI operation (method, path, operation ID, summary, tags, `secured`) and the plain handlers outside the document (health checks, docs, OpenAPI files, well-known files), `source` telling which |
| `GET /_dev/config` | All | The environment variables the app read at startup: `name`, `secret`, `set`, and `value` only for variables that aren't secret |
| `GET /_dev/requests` | All | The 500 most recent requests, newest first |
| `GET /_dev/requests/stream` | All | Server-Sent Events: each request as it finishes |
| `GET /_dev/logs` | All | The 1,000 most recent log records at info level and above, newest first |
| `GET /_dev/logs/stream` | All | Server-Sent Events: each log record |
| `GET /_dev/mail` | Full, with `MAIL_DELIVERY=mailpit` | The 50 newest messages in Mailpit (sender, recipients, subject, snippet, time, size) and Mailpit's web address to read them; 503 `unavailable` when Mailpit doesn't answer. With the default `devmail`, the inbox is the Dev Portal's (`/_portal/api/mail`, [ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)) |
| `GET /_dev/mail/previews` | Full | The app's email previews: name, description, category. A v0.1 app lists them in `internal/app/mail_previews.go`; in an app on `gorbital.Main` they come from the modules, which add them during `Setup` through `gorbital.AuthSetup.MailPreviews` — sign-in's own messages come from `authhttp` that way — plus the plain test message the library adds |
| `GET /_dev/mail/preview?name=&to=` | Full | One preview rendered with sample data for `to` (default `preview@example.com`): subject, text, HTML; 404 `preview_not_found` |
| `POST /_dev/mail/preview/send?name=&to=` | Full | Sends the rendered preview through the app's mailer, so it lands in the development inbox; the console's one POST endpoint |
| `GET /_dev/migrations` | Full | `{"current", "latest", "pending"}` |
| `GET /_dev/jobs` | Full | The 50 most recent jobs, newest first, without their arguments: kind, queue, state, attempts, times, error messages, request ID |

An endpoint the app doesn't have (such as `/_dev/mail` in a Minimal app) answers 404 and isn't in the index.

Built-in modules can add endpoints behind the same checks (`devconsole.Sources.Extensions`, `gorbital.AuthSetup.DevEndpoints`); the index lists their prefixes as `extensions`. Sign-in from `gorbital.dev/gorbital/authhttp` serves its tests under `/_dev/auth/test/` ([Testing sign-in](../dev-portal/testing-sign-in.md#where-it-comes-from), [ADR-0087](../adr/0087-testing-sign-in-from-the-dev-portal.md)).

### Requests

```json
{
  "max": 500,
  "requests": [
    {"time": "2026-09-16T10:00:01.52Z", "method": "GET", "route": "/v1/projects/{id}", "path": "/v1/projects/prj_01J…",
     "status": 200, "duration_ms": 3.41, "request_id": "req_4d6fe7eb6c86a735", "trace_id": "af4bc7b8d2a26bccd0ece63aaebc2e68"}
  ]
}
```

Requests never include query strings, headers, cookies or bodies. `route` is the pattern the router matched (empty when a middleware answered first, `/` for unknown paths). The console's own requests aren't listed. In Full apps the list comes from the same collector as [live observability](observability.md), so it includes requests answered by maintenance mode, rate limits and authentication.

### Logs

```json
{
  "max": 1000,
  "logs": [
    {"time": "2026-09-16T10:00:01.52Z", "level": "INFO", "message": "http request",
     "attrs": [{"key": "method", "value": "GET"}, {"key": "route", "value": "GET /v1/ping"}, {"key": "status", "value": "200"},
               {"key": "request_id", "value": "req_4d6fe7eb6c86a735"}, {"key": "trace_id", "value": "af4bc7b8…"}]}
  ]
}
```

Records are copies of what the app logs, taken at info level and above whatever `APP_LOG_LEVEL` says. Values print as they do in the log (secrets typed `config.Secret` show `[redacted]`), groups become dotted keys, and records are bounded: messages to 4 KiB, 50 attributes of at most 1 KiB each and 8 KiB in all, with `dropped_attrs` counting the rest.

### Configuration

```json
{
  "variables": [
    {"name": "APP_ADDR", "secret": false, "set": true, "value": "127.0.0.1:8080"},
    {"name": "DATABASE_URL", "secret": true, "set": true},
    {"name": "GITHUB_CLIENT_SECRET", "secret": true, "set": false},
    {"name": "OTEL_EXPORTER_OTLP_ENDPOINT", "secret": false, "set": true, "value": "https://[redacted]@otel.example.com"}
  ]
}
```

The list is exactly what `LoadConfig` read, recorded as it read it: variables read as secrets show only whether they're set, and so does any variable whose name contains `SECRET`, `PASSWORD`, `TOKEN`, `KEY`, `PRIVATE`, `CREDENTIAL`, `DSN` or `DATABASE_URL`, even if the app reads it as plain configuration. In other values, a URL's user information, query and fragment become `[redacted]`. Values come from startup; the console never reads the environment again.

### Streams

```text
retry: 3000

event: request
data: {"time":"…","method":"GET","route":"/v1/ping","path":"/v1/ping","status":200,"duration_ms":0.42,"request_id":"req_…"}

: keep-alive

event: dropped
data: {"count":12}

event: end
data: {"reason":"max_duration"}
```

A stream sends each new item (`request` or `log`), a `: keep-alive` comment every 15 seconds, `dropped` when the client fell more than 256 events behind, and a final `end` after 30 minutes (`max_duration`) or when the app shuts down (`shutdown`). At most 8 streams run at once; more get 429 `rate_limited`. Start a stream, then fetch the list, to see everything without a gap.

## Troubleshooting

| Symptom | Cause |
|---|---|
| 404 `no route matches GET /_dev/…` | The console is off: run through `orb dev`, or set `DEV_CONSOLE_TOKEN` with `APP_ENV=development`. Apps created with a development build before the dev console need the upgrade ([upgrade notes](upgrade-notes.md#before-v010-development-builds)) |
| 403 `forbidden` | Use `http://127.0.0.1:<port>`, `http://localhost:<port>` or `http://[::1]:<port>`, from the same machine. Proxies must send one of those as `Host` and add no forwarding headers (`X-Forwarded-For` and the like); requests through a tunnel are always refused |
| 401 `unauthorized` | The token is missing or from an earlier `orb dev` run |
| 503 on `/_dev/mail` | Mailpit isn't running, or `MAILPIT_WEB_PORT` doesn't match `compose.yaml`; with `MAIL_DELIVERY=devmail` the endpoint isn't served at all (the inbox is the portal's) |
| Stream shows nothing through a proxy | The proxy buffers the response; `X-Accel-Buffering: no` is set for nginx |

## In your app

In a v0.1 app the wiring is in `internal/app/devconsole.go`, and the checks and buffers are in `gorbital.dev/modules/devconsole`. To list more plain handlers in `/_dev/routes` when you add them in `routes.go`, add their paths to `handlerRoutes`; `TestDevConsoleRoutes` checks that each listed route is served.

An app on `gorbital.Main` has none of this in its own code: the library wires the console in `gorbital/devconsole.go`, from the same `modules/devconsole`, and lists the handlers modules registered through `gorbital.AuthSetup.Handle`.
