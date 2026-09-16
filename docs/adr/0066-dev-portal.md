# ADR-0066: Dev Portal

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0010, ADR-0021, ADR-0028, ADR-0029, ADR-0065

## Context

v1.1 gave apps development-only APIs under `/_dev/` ([ADR-0065](0065-local-dev-console-apis.md)) and left the Dev Portal in gorbital-dashboards on mock data. The portal is meant to be the place where a developer does everything they do today with commands, flags and hand-edited files: see the app's state, routes, jobs, logs and email; generate jobs, resources and migrations; edit the database, its schema and the environment; run queries; and later manage authentication, storage, git and observability. The full plan is in the [Dev Portal roadmap](../dev-portal-roadmap.md). Today:

| Area | Today | Evidence |
|---|---|---|
| The portal UI | Three Next.js apps in gorbital-dashboards (Dev Portal, Observability Portal, Deployment Portal) sharing one theme and component package; every screen renders from `apps/*/lib/mock.ts`; static exports served from `out/` and deployed on Vercel | gorbital-dashboards README |
| App-side APIs | `/_dev/` behind Host, loopback and token checks, GET only, no CORS: a browser on another origin can't call them directly; the guide says to serve a UI from the app's origin or proxy from the UI's server | ADR-0065, `docs/guides/dev-console.md` |
| `orb dev` | Builds and runs the app, watches files, applies migrations, starts Docker services; prints a banner; the app's output goes to the terminal only; nothing can ask it to restart | `cli/internal/cli/dev.go` |
| Generators | `orb gen job`, `orb gen resource` and `orb gen migration` render, check the app, then write, with `--dry-run` listing paths; the rendered content isn't available before writing | `cli/internal/cli/gen*.go`, ADR-0021 |
| Where the console belongs | ADR-0010 puts the local dev console with the CLI tooling; ADR-0028 says a custom dev console may replace Grafana for local viewing | ADR-0010, ADR-0028 |
| Threat model | Row 11: localhost services bound to `127.0.0.1`; a custom dev console checks the `Host` header and uses a session token | ADR-0029 |

## Options

### Who serves the portal

| Option | Verdict |
|---|---|
| The app, mounting the UI next to `/_dev/` | Rejected: the UI must change the developer's files, run `git` and `go`, and restart the app; the app can't do that to itself, and a production binary must never carry any of it |
| A separate `orb portal` process | Rejected: two processes to start, and the portal needs what `orb dev` already has (the app's process, output and reload loop) |
| **`orb dev` serves it: the UI, an API of its own under `/_portal/api/`, and a proxy under `/_portal/app/` to the app** | **Chosen**: one command, one process that owns the app; the UI is a static export embedded in `orb` (`cli/internal/portal/ui`), so users need no Node |

### Where the UI's code lives

| Option | Verdict |
|---|---|
| Move `apps/devtools` into this repository | Rejected for now: the three portals share one theme and component package, and gorbital-web takes their screenshots; splitting them costs more than a build step |
| **Keep it in gorbital-dashboards; `scripts/sync-portal.sh` builds it and copies the export into `cli/internal/portal/ui/dist`, which `go:embed` picks up; only `dist/.gitkeep` is committed; a checkout without the build serves a placeholder page that says how to get the UI** | **Chosen**: the release workflow runs the script at a pinned gorbital-dashboards ref, so released binaries carry the UI, while every Go test and `go install` from a plain checkout still works |

### Protecting the portal

The portal can read the developer's project and write to it, so its API needs the same protection the dev console has, and one more check because it accepts writes.

| Option | Verdict |
|---|---|
| No authentication, like Supabase's local Studio | Rejected: any page in the developer's browser could reach `http://127.0.0.1:3100` and, with DNS rebinding, read answers and send writes |
| The dev console token, shared with the UI | Rejected: the UI would hold a token that reads the app's configuration and logs; a leak in the UI is a leak of the app's token |
| **A portal token per run, delivered in the link `orb dev` prints and opens (`/_portal/auth?t=…`), which sets an `HttpOnly`, `SameSite=Strict` cookie; every API and proxy request needs a loopback `Host`, a loopback peer, the cookie or an `Authorization: Bearer` header, and, for anything but GET and HEAD, an `X-Orb-Portal` header; never CORS headers; `Cache-Control: no-store`** | **Chosen**: the Host check defeats DNS rebinding; a custom header on writes forces a CORS preflight the portal never answers, so a cross-origin page can't send a write even if a cookie were sent; the console token stays inside `orb dev`, which adds it to proxied `/_dev/` requests |
| Checking the `Host` port too, as the console does | Not done: the UI in development runs on its own port (3101) and proxies to the portal; any loopback name is accepted with any port. Nothing else on the machine is meant to answer a portal request, and the token still applies |

### Generators from the UI

| Option | Verdict |
|---|---|
| Run `orb gen` as a subprocess and parse its output | Rejected: no preview of the content, and two code paths for one operation |
| **Every generator plans before it writes (`cli/internal/genplan`): `planJob`, `planResource` and `planMigration` return the file changes (create with content; modify with before and after); `orb gen` prints or applies the plan, the portal shows it as a diff and applies it on request; applying re-checks that each file is as the plan saw it** | **Chosen**: one engine, two front ends (ADR-0021's operation model with a plan in the middle); a stale plan is refused instead of overwriting edits made since |

### The app's process

| Option | Verdict |
|---|---|
| The UI polls the app | Rejected: the app can't say it is building or stopped |
| **`orb dev` becomes a supervisor: it keeps the app's state (preparing, building, running, stopped, with the last problem), copies the app's and its own output into a ring buffer, streams both as Server-Sent Events, and takes restart, stop and start requests through a command channel its loop serves between change checks** | **Chosen**: the terminal shows exactly what it did before; the portal sees the same, plus the state |

### `/ops/` from the portal

The portal proxies `/ops/` too, but those endpoints need a signed-in `platform_admin` with two-factor authentication. Phase 1 of the roadmap decides how the portal acts there: the intended design is a development-only principal that the app grants to requests carrying the dev console token, recorded in audit events as the dev console, and refused in production like the token itself. It is an amendment to this record, not part of it.

## Decision

### `cli/internal/portal`

| Piece | Decision |
|---|---|
| `New(Config)` | Refuses tokens outside 32–512 visible ASCII characters (`CheckToken`, `ErrInvalidToken`) and a config without a supervisor or hub. Config: token, orb version, project (from `gorbital.yaml` and `go.mod`), supervisor, hub, console token, links, generators, the embedded UI, a log function, stream limits (8 at once, 30 minutes) |
| Checks, in order | `Host` names `localhost`, `127.0.0.1` or `[::1]` (any port), else 403 `forbidden`; peer address loopback, else 403; the `orb_portal` cookie or `Authorization: Bearer` equal to the token, compared as SHA-256 hashes in constant time, else 401 `unauthorized` with `WWW-Authenticate`; for methods other than GET and HEAD, the `X-Orb-Portal` header, else 403. Refusals log at most once a second |
| `GET /_portal/auth?t=<token>` | Loopback checks, then the token: sets the cookie (`HttpOnly`, `SameSite=Strict`, `Path=/`) and redirects to `/`; a wrong token gets a page saying the link is from another run, and no cookie |
| `GET /_portal/api/status` | Portal (version, whether a UI is bundled, start time), project (name, module, preset, tenancy, features, mail, directory, database), the app's status, links (api, docs, mail, console, grafana as they apply), generator names |
| `GET /_portal/api/output?limit=` | The most recent output lines (2,000 kept), oldest first, each with time, stream (`app` or `orb`) and text; lines over 8 KiB are cut |
| `GET /_portal/api/events` | Server-Sent Events: the current state first, then `state` and `output` events, `dropped` with a count when a client falls 256 events behind, `: keep-alive` every 15 s, a final `end` (`max_duration` or `shutdown`); 429 `rate_limited` over the limit |
| `POST /_portal/api/app/restart`, `stop`, `start` | Queue the command for `orb dev`'s loop and answer 202 with the status as it is; 409 `app_action_failed` while an earlier command waits |
| `POST /_portal/api/generators/{job\|resource\|migration}/plan` and `/apply` | Body `{"input": {…}, "allow_dirty": bool}`; input fields are the CLI flags' names with underscores, unknown fields refused; `plan` answers the plan without writing, `apply` plans again and writes after the clean-git check (skipped with `allow_dirty`); 422 `generator_failed` for input or app problems, 409 `plan_conflict` when a file changed since |
| `/_portal/app/…` | Reverse proxy to the app's address as the supervisor reports it (a wildcard host becomes `127.0.0.1`), with `Host` set to that address; drops the portal's cookie and header; adds `Authorization: Bearer <console token>` to `/_dev/` requests that carry none; flushes immediately, so the console's streams pass through; 502 `app_unavailable` when the app doesn't answer |
| The UI | A Next.js static export served at `/`: `/modules` is `modules.html`, `_next/static/` is immutable, unknown paths get the export's 404 page; a build without the UI serves a placeholder page. Pages are open to any local reader (they hold nothing secret); the API isn't |
| Responses | problem+json errors shaped like `httpx.Problem` (title, status, code, detail), `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`; never CORS headers |

### `cli/internal/genplan`

| Piece | Decision |
|---|---|
| `Plan` | Generator, name, summary (the CLI's confirmation text), changes in write order, next steps, and the generator's `--json` result |
| `Change` | Path (slash-separated, relative to the app), kind `create` or `modify`, content, and for `modify` the content before; JSON carries content as strings |
| `Check(dir, plan)` | A `create` whose file exists → `ErrExists`; a `modify` whose file differs from `Before` → `ErrStale` |
| `Apply(dir, plan)` | `Check`, then writes through `os.Root` (ADR-0029, threat 3), creating directories |

### `orb dev`

| Piece | Decision |
|---|---|
| Flags | `--portal-port` (default `DEV_PORTAL_PORT` from `.env` or the environment, else 3100), `--no-portal`, `--no-open` |
| Token | `DEV_PORTAL_TOKEN` from `orb dev`'s environment when set (checked, not printed), else 256 random bits per run; never written to disk; the same for reloads within one run |
| Banner | `✓ Dev Portal http://127.0.0.1:3100/_portal/auth?t=<token> (docs/guides/dev-portal.md)`; the link opens in the browser when output is a terminal and `CI` is unset, unless `--no-open` |
| Port | Checked like the services' ports before anything starts; a conflict names `DEV_PORTAL_PORT`, `--portal-port` and `--no-portal` |
| Supervisor | `devRunner` implements `portal.Supervisor`: `Status` (state, PID, address, start time, restarts, last problem, whether the console is on), `Restart`, `Stop`, `Start` through a one-slot command channel; the loop serves commands between change checks; a failed build or migration keeps the previous version running and records the problem |
| Output | The app's standard output and error still reach the terminal; a copy of each line, and of `orb`'s own messages, goes to the portal's hub |
| Generators | `orb gen job`, `resource` and `migration` build a plan and apply it with `genplan.Apply`; the portal's generators call the same plan functions with the same validation; `--dry-run` and `--json` output are unchanged |

### gorbital-dashboards

| Piece | Decision |
|---|---|
| Data | A React Query client for `/_portal/api/` and `/_portal/app/` with the cookie and the write header; a mock transport answering the same endpoints from `lib/mock.ts`, selected by `NEXT_PUBLIC_DEVTOOLS_DATA=mock` (the public demo and gorbital-web's screenshots) or `localStorage.devtoolsData`; live is the default |
| Development | `pnpm devtools dev` on port 3101 with rewrites of `/_portal/` to `orb dev` on 3100 (dev server only; the export has none) |
| Primitives | Inputs, dialogs, side sheets, dropdowns, tabs, tooltips, toasts and a command palette on Radix primitives, styled with the existing tokens; buttons, pills, segmented controls and tables become interactive |
| Screens in Phase 0 | An Overview page on live data (state, project, links, readiness, the app's output as it happens, restart, stop and start); the other screens stay on mock data until Phase 1 |

## Why

- One process: `orb dev` already owns the app; giving it an HTTP server costs less than teaching a second process to find the app, its token and its output.
- Embedding: a developer who installs `orb` gets the portal; a contributor who clones the repository still builds and tests without Node.
- Plans: a UI that shows a diff before writing is only honest if the diff is what gets written. Making the CLI apply the same plan removes the second code path and gives `--dry-run` real content for free.
- The security checks mirror ADR-0065 because the threat is the same (a page in the developer's browser reaching a local service), and add the write header because the portal, unlike the console, accepts writes.
- A separate portal token keeps the app's console token where it was: in `orb dev`'s memory and the app's environment.

## Trade-offs

- The UI's source and the binary that embeds it live in two repositories; a release of `orb` pins a gorbital-dashboards ref. A version mismatch shows as a missing endpoint, which the UI reports as such.
- The portal listens on a second port (3100), which may collide with other tools; `DEV_PORTAL_PORT` moves it and `--no-portal` turns it off.
- Anything running as the developer's user can read `orb dev`'s environment and memory, and so both tokens. The portal assumes the user account isn't compromised, as the console does.
- Only the token in the link signs a browser in; a developer who closes the link before it opens must copy it from the terminal.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)) row 11 now covers the portal: loopback binding, Host check, per-run token, write header; the app's `/_dev/` token never reaches the browser.
- ADR-0021's generator operation model gains the plan step; ADR-0010 and ADR-0028 get the console they anticipated.
- Guides: [Dev Portal](../guides/dev-portal.md) (new), [CLI](../guides/cli.md) (`orb dev` flags), [dev console](../guides/dev-console.md) (the proxy), [local development](../guides/local-development.md) (building `orb` with the UI).
- Upgrade notes: nothing for existing apps; `DEV_PORTAL_PORT` is optional.
- The [Dev Portal roadmap](../dev-portal-roadmap.md) tracks the remaining phases; each phase that adds a public surface (an `/ops` principal, new app endpoints, new modules) amends this record or adds its own.

## Implementation notes (2026-09-16)

Phase 0 of the roadmap, on the branches `dev-portal/phase-0` of both repositories:

- `cli/internal/portal`: the server, guard, auth link, API, events stream, proxy and static handler, with tests for every refusal, the cookie, the stream, the proxy's headers and the export's path mapping.
- `cli/internal/genplan`: plans and `Apply`, with tests for stale and existing files and for paths that try to leave the app.
- `cli/internal/cli`: `planJob`, `planResource`, `planMigration` (`gen_plan.go`) used by both `orb gen` and the portal; `dev.go` as a supervisor; `dev_portal.go` for the token, port, project, links, generator wiring and the browser; tests that the portal's plan lists the same files as `orb gen job --dry-run --json` and that `apply` writes them.
- `scripts/sync-portal.sh` and a step in `release-cli.yml`; `cli/internal/portal/ui` embeds `dist/`.
- gorbital-dashboards: the data layer, mock transport, primitives, shell changes, the Overview page and the Routes page moved to `/routes`.
