# Dev Portal

While you run an app with `orb dev`, it also serves the Dev Portal at http://127.0.0.1:3100: a web UI for the app on your bench. It shows what the app is doing and its output as it happens, lets you restart it, and reaches the app's development APIs (routes, requests, logs, email, jobs, configuration) without you holding any token. Later phases add the database, SQL, authentication, storage, git and more; the plan is in the [Dev Portal roadmap](../dev-portal-roadmap.md). Decision: [ADR-0066](../adr/0066-dev-portal.md).

The portal exists only while `orb dev` runs, only on this machine, and only in development. Nothing about it is in the app or in production builds.

## Opening it

`orb dev` prints a link and opens it in your browser:

```text
  ✓ API        http://127.0.0.1:8080
  ✓ API docs   http://127.0.0.1:8080/docs
  ✓ Emails     http://127.0.0.1:8025
  ✓ Dev APIs   http://127.0.0.1:8080/_dev/ (docs/guides/dev-console.md)
    Token      q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E (Authorization: Bearer; new on every orb dev run)
  ✓ Dev Portal http://127.0.0.1:3100/_portal/auth?t=Rk1…vQ (docs/guides/dev-portal.md)
```

The link holds this run's portal token. Opening it once sets a cookie in your browser, and the portal at http://127.0.0.1:3100 works for the rest of the run. The next `orb dev` run prints a new link; a link from an earlier run shows a page saying so.

| Flag or variable | Effect |
|---|---|
| `--no-open` | Print the link without opening a browser (also the behaviour in CI and when output isn't a terminal) |
| `--no-portal` | Don't serve the portal |
| `--portal-port 3110` | Listen on another port |
| `DEV_PORTAL_PORT=3110` in `.env` | The same, for good |
| `DEV_PORTAL_TOKEN` in the shell running `orb dev` | Use that token instead of a random one, for a tool that needs the same token across runs; it must be 32 to 512 visible ASCII characters and is never written to `.env` |

A taken port stops `orb dev` before anything starts, naming the port and the ways to move it.

## What it serves

| Path | What |
|---|---|
| `/` and every page | The portal's UI, a Next.js static export embedded in `orb` (built from [gorbital-dashboards](https://github.com/gorbital/gorbital-dashboards)). Pages are open to any local reader and hold nothing secret; every request for data needs the token |
| `/_portal/auth?t=<token>` | Signs the browser in: sets the `orb_portal` cookie and goes to `/` |
| `/_portal/api/…` | The portal's own API: the app's state and output, restarts, generators (below) |
| `/_portal/app/…` | A proxy to the app: `/_portal/app/v1/ping` is the app's `/v1/ping`. Requests to `/_portal/app/_dev/…` and `/_portal/app/ops/…` that carry no `Authorization` get the [dev console](dev-console.md) token added by `orb dev`, so the UI never sees it: in Full apps the token acts as the development operator on `/ops/` ([ADR-0066](../adr/0066-dev-portal.md)). Other requests go through with the headers and cookies you send, minus the portal's own |

Every API and proxy request must pass, in order: a `Host` header naming `localhost`, `127.0.0.1` or `[::1]` (403 otherwise: this defeats DNS rebinding, since a page on another site that points its own name at `127.0.0.1` still sends its own name); a connection from this machine (403); the token, as the cookie or as `Authorization: Bearer <token>` (401); and, for anything but GET and HEAD, an `X-Orb-Portal` header (403), which a browser sends only after a CORS preflight the portal never answers. Responses never carry CORS headers.

### Calling the API yourself

```bash
TOKEN=Rk1…vQ                       # from the link orb dev printed
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/api/status
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:3100/_portal/api/output?limit=50'
curl -N -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/api/events
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Orb-Portal: 1" http://127.0.0.1:3100/_portal/api/app/restart
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/app/_dev/routes
```

| Endpoint | Returns |
|---|---|
| `GET /_portal/api/status` | `portal` (orb version, whether a UI is bundled, start time), `project` (name, module, preset, tenancy, features, mail provider, directory, whether it has a database), `app` (below), `links` (`api`, `docs`, and `mail`, `console`, `grafana` when they apply), `generators` (names) |
| `GET /_portal/api/output?limit=200` | The most recent lines the app and `orb dev` wrote, oldest first: `{"time", "stream": "app"\|"orb", "text"}`. `orb dev` keeps 2,000 |
| `GET /_portal/api/events` | Server-Sent Events: a `state` event first, then `state` and `output` events as they happen, `: keep-alive` every 15 seconds, `dropped` with a count when the client fell behind, and a final `end` after 30 minutes or when `orb dev` stops. At most 8 streams at once |
| `POST /_portal/api/app/restart` | Rebuilds and restarts the app; 202 with the status. `stop` ends the process and leaves it stopped until `start` or a file change; `start` starts a stopped app without rebuilding; `migrate` applies pending migrations (`go run ./cmd/migrate`) without a restart, 409 in an app without a database. 409 while an earlier request is still being handled |
| `POST /_portal/api/generators/{job\|resource\|migration}/plan` | Body `{"input": {…}}`. Answers the plan: every file the generator would write, with its content (and the current content of files it changes), the summary `orb gen` shows, and the next steps. Nothing is written |
| `POST /_portal/api/generators/{name}/apply` | The same body, plus `"allow_dirty": true` to skip the clean-git check. Plans again and writes; 409 `plan_conflict` if a file changed since the plan |

The app's status:

```json
{"state": "running", "pid": 48213, "addr": "127.0.0.1:8080", "url": "http://127.0.0.1:8080",
 "started_at": "2026-09-16T10:00:01Z", "restarts": 2, "console": true}
```

`state` is `preparing` (services, migrations, seed data), `building`, `running` or `stopped`; `problem` holds the last build or migration failure until the next success (the previous version keeps running meanwhile, as it does in the terminal); `console` says whether the app serves `/_dev/`.

Generator inputs are the flags of `orb gen job`, `orb gen resource` and `orb gen migration` with underscores: `{"name": "CleanupSessions", "schedule": "30 2 * * *", "timeout": "5m", "max_attempts": 8, "queue": "maintenance"}`, `{"name": "Project", "fields": ["name:string:unique", "status:enum(active,archived)"], "scope": "user"}`, `{"name": "add_customer_phone"}`. Defaults and validation are the CLI's; unknown fields are refused. `orb gen … --dry-run` is the same plan printed.

## Screens

| Screen | Shows | From |
|---|---|---|
| Overview | The app's state, uptime, restarts, readiness, health checks, project, links, output as it happens; restart, stop, start | `/_portal/api/status`, `/_portal/api/events`, `/readyz`, `/ops/system` |
| Routes | Every route with its method, path, summary, tags and security, and a request builder that sends through the proxy | `/_dev/routes` |
| Requests | Recent requests and a live tail, with the log records of a request | `/_dev/requests`, `/_dev/requests/stream`, `/_dev/logs` |
| Logs | Recent log records and a live tail, by level and text | `/_dev/logs`, `/_dev/logs/stream` |
| Modules | What the app wired: libraries, API modules, jobs, settings, flags, permission catalogs | `/_dev/app` |
| Audit | The audit log with filters and statistics | `/ops/audit`, `/ops/audit/stats` |
| Jobs | Definitions, runs, queues; run now, retry, cancel, enable, disable, reschedule, pause and resume | `/ops/jobs/…`, `/ops/queues` |
| Settings | Runtime settings by group with history; change and reset | `/ops/settings` |
| Database | Migration state, pool, health checks; apply pending migrations | `/_dev/migrations`, `/ops/system`, `/_portal/api/app/migrate` |
| Mail | Captured email, delivery settings, a test email, the suppression list | `/_dev/mail`, `/ops/mail` |

Screens that need `/ops/` show an explanation in an app whose `orb` predates the development operator, and Minimal apps show what they have.

## Building `orb` with the UI

A plain checkout of gorbital builds an `orb` whose portal serves a placeholder page: the API and proxy work, and the page says how to get the UI. Released binaries carry it. To embed the UI you are working on:

```bash
cd gorbital
scripts/sync-portal.sh            # builds ../gorbital-dashboards/apps/devtools and copies the export into cli/internal/portal/ui/dist
cd cli && go install ./cmd/orb
```

`scripts/sync-portal.sh /path/to/gorbital-dashboards` uses another checkout; `DASHBOARDS_REF=<tag>` checks that ref out first. Only `dist/.gitkeep` is committed; the copied files are ignored by git. The script needs Node 22 and pnpm 10.

To work on the UI itself, run it from its source with live reload instead of rebuilding `orb` on every change:

```bash
cd gorbital-dashboards
pnpm install
pnpm devtools dev                 # http://localhost:3101, with orb dev running in your app
```

The development server proxies `/_portal/` to `orb dev` on port 3100 (`ORB_PORTAL_URL` to change it), so the UI at 3101 talks to the real app. Sign in once by opening the link `orb dev` printed, replacing the port with 3101, or open the portal at 3100 first: the cookie is set for `127.0.0.1` and both ports see it.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `port 3100 for the Dev Portal is already in use` | Another program listens there: add `DEV_PORTAL_PORT=3110` to `.env`, pass `--portal-port`, or `--no-portal` |
| The page says the link is from another `orb dev` run | Use the link the running `orb dev` printed; every run has a new token |
| 401 `unauthorized` from the API | No cookie and no bearer token; open the link, or send `Authorization: Bearer <token>` |
| 403 `forbidden` | The request came through another host name, from another machine, or is a write without `X-Orb-Portal`. Use `http://127.0.0.1:3100` or `http://localhost:3100` from this machine |
| 502 `app_unavailable` from `/_portal/app/…` | The app isn't running (see its state and output on the Overview page), or `APP_ADDR` changed and it hasn't restarted |
| The portal shows a placeholder page | This `orb` was built without the UI: run `scripts/sync-portal.sh` and reinstall, or run the UI from source (above) |
| `/_portal/app/_dev/…` answers 404 | The app has no dev console: it runs without `DEV_CONSOLE_TOKEN`, or predates v1.1 ([upgrade notes](upgrade-notes.md)) |
