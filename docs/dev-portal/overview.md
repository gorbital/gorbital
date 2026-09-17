# Overview

The Overview is where the link lands: the app's state and output as they happen, its health, the project as `orb dev` reads it, and the buttons that restart, stop or start the app. It reads the portal's own API, so it works while the app is stopped or failing to build.

![The Overview screen](screenshots/overview.png)

## What you see

| Panel | What it shows |
|---|---|
| Header | The app's name and directory; the `orb` version and whether a UI is bundled; Restart, Stop, Start |
| App | The state (`preparing`, `building`, `running`, `stopped`), the PID and the address |
| Uptime | Since the app's last start |
| Restarts | Since `orb dev` started |
| Readiness | The answer of `GET /readyz`, polled every 10 s |
| Output | The app's and `orb dev`'s lines as they arrive, with a filter (All, App, orb), Load more (older lines, up to the 2,000 `orb dev` keeps) and Clear (the view only) |
| Project | Name, module, preset, tenancy, database, mail provider, directory, and a chip per feature |
| Links | The API, the dev console and the API docs; the inbox and Grafana when they apply |
| Health | The checks `/ops/system` reports: PostgreSQL with its latency, the migration state |

While a build or a migration fails, the last error is shown and the previous version keeps running, as it does in the terminal.

## What you can do

| Action | What it does |
|---|---|
| Restart | Rebuilds and restarts the app; the state goes through `building`. Refused (409) while an earlier command is still being handled |
| Stop | Ends the process and leaves it stopped until Start or a file change |
| Start | Starts a stopped app without rebuilding |
| Load more, Clear | Fetches older output lines; empties the view. Nothing on disk changes |

None of these asks for a confirmation, and none touches the app's files.

## Where it comes from

`GET /_portal/api/status`, `GET /_portal/api/output`, `GET /_portal/api/events` (a Server-Sent Events stream: the state first, then every output line and state change), `POST /_portal/api/app/restart`, `stop` and `start`; through the proxy, `/readyz` and `/ops/system`. Decided in [ADR-0066](../adr/0066-dev-portal.md); the endpoints are listed in the [Dev Portal guide](../guides/dev-portal.md).

## Notes

- The stream ends after 30 minutes or when `orb dev` stops; the page reconnects by itself. At most 8 streams at once.
- Output lines over 8 KiB are cut. The full records are on [Logs](requests-and-logs.md#logs).
- Health reads `/ops/`, so it is empty in a Minimal app; the state, the output and the buttons work everywhere.
