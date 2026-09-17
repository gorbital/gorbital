# ADR-0086: Dev Portal tunnel

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0065 (the dev console also refuses forwarded requests), ADR-0066 (the portal runs cloudflared) · **Builds on:** ADR-0052, ADR-0074, ADR-0077

## Context

Phase 11 of the [v0.2 roadmap](../v0.2-roadmap.md#phase-11-dev-portal-tunnel-and-live-sign-in-tests): developers want to expose their local app with one click from the Dev Portal, to receive webhooks (Resend, payment providers), try the API from a phone, and test Google, Apple and GitHub sign-in and passkeys on a real HTTPS domain, which `localhost` can't give them (Apple refuses `http` return URLs, passkeys need a registrable domain for anything but `localhost`, a phone can't reach `127.0.0.1`).

What exists:

| Piece | Today | Evidence |
|---|---|---|
| `orb dev` | Supervises the app process (its own process group, `SIGINT` then `SIGKILL`), serves the portal on `127.0.0.1:3100`, streams state over SSE | `cli/internal/cli/dev.go`, [ADR-0066](0066-dev-portal.md) |
| The portal's guard | `Host` must name a loopback name, the peer must be loopback, the per-run token, `X-Orb-Portal` on writes | `cli/internal/portal/portal.go` |
| The dev console (`/_dev/*`) and the dev operator on `/ops/` | `Host` must be `localhost`/`127.0.0.1`/`[::1]` with the port the request arrived on, peer loopback, the console token | `modules/devconsole`, [ADR-0065](0065-local-dev-console-apis.md) |
| Sign-in configuration | `APP_PUBLIC_URL`, `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS`, `AUTH_DEFAULT_RETURN_TO`, `APP_CORS_ORIGINS`, `APP_TRUSTED_PROXIES` | `gorbital/config.go`, `gorbital/config_auth.go`, [auth providers](../guides/auth-providers.md) |
| `.env` edits | The env editor rewrites `.env`; `orb dev` watches `.env` and rebuilds | [ADR-0074](0074-dev-mail-previews-and-env-editor.md) |

A tunnel (`cloudflared`) runs on the developer's machine and connects to the app from loopback. That is the crux of the security question: every local check that trusts "the peer is loopback" is satisfied by a request from the internet.

## Decisions

### 1. The developer's cloudflared

| Option | Verdict |
|---|---|
| Bundle or download cloudflared | Rejected: a binary orb would have to verify, update and license; a download on first use is a supply-chain step the developer didn't ask for |
| ngrok, localtunnel, Tailscale Funnel | Not now: accounts or tokens for everything, or no stable hostname without payment; Cloudflare's quick tunnels need no account and named tunnels are free on a zone the developer already has |
| **Run `cloudflared` from `PATH` (or `ORB_CLOUDFLARED`), never download it; missing, the portal and the CLI print install steps per OS (`brew install cloudflared`, Cloudflare's apt/rpm repositories and release binaries, `winget install --id Cloudflare.cloudflared`)** | **Chosen** |

### 2. Two modes

| Mode | Command | For |
|---|---|---|
| Quick | `cloudflared tunnel --url http://127.0.0.1:<app port> --no-autoupdate` | Webhooks and phones. A random `*.trycloudflare.com` URL that changes on every start: not for OAuth callbacks or passkeys, and the screen and the setup say so |
| Named | `cloudflared tunnel --no-autoupdate run` with `TUNNEL_TOKEN` in its environment | Sign-in and passkeys: the developer's tunnel from the Cloudflare dashboard, whose public hostname points at the app |

The quick tunnel's URL is parsed from any line of cloudflared's output: the first `https://<one DNS label>.trycloudflare.com`, lower-cased, never `api.trycloudflare.com`, nothing with a port or user info, and independent of the surrounding wording (cloudflared draws it inside a box). The parser is fuzzed: whatever it returns matches `^https://[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.trycloudflare\.com$` and is in the line. The tunnel is connected when cloudflared logs `Registered tunnel connection`; a start that doesn't reach it in 45 seconds fails with cloudflared's last error, and cloudflared is stopped.

**Deviation from the brief:** the brief said `cloudflared tunnel run --token <token>`. The token goes in `TUNNEL_TOKEN` (cloudflared's own variable for `--token`) instead, because a command line is readable by every user of the machine (`ps`), and the environment of another user's process isn't. `TUNNEL_TOKEN`, `TUNNEL_TOKEN_FILE`, `TUNNEL_URL` and the `CLOUDFLARE_TUNNEL_TOKEN*` variables are removed from what cloudflared inherits otherwise.

### 3. The token and the hostname

- The token is read from `CLOUDFLARE_TUNNEL_TOKEN`, or the file `CLOUDFLARE_TUNNEL_TOKEN_FILE` names (both set is an error), from `orb dev`'s environment or `.env`; it is checked to be base64 of a JSON object with a tunnel ID and a secret, with errors that never include it. It is never logged, never in a portal answer (the portal says which variable is set, or what is wrong with it), never written anywhere but where the developer put it, and removed (with the secret decoded from it) from every line of cloudflared's output that orb keeps, streams or prints. `orb dev` also removes both variables from the app's environment: the token is cloudflared's.
- The hostname comes from `--tunnel-hostname`, the portal's field, `ORB_TUNNEL_HOSTNAME`, or the one saved from the portal in `.orb/portal/tunnel.json` (0600, git-ignored, not a secret). It is normalized (`https://Dev.Example.com/` → `dev.example.com`) and refused when it isn't a public DNS name: an IP, `localhost`, a single label, a port or path, a `trycloudflare.com` name.

### 4. Only the app goes through the tunnel

The quick tunnel's target is the app's address from `APP_ADDR` (a wildcard host becomes `127.0.0.1`). A named tunnel's target is configured in the Cloudflare dashboard, which orb can't see; the screen shows the value its public hostname's service must have, and the reachability check requests `https://<hostname>/livez` and compares the answer (status and body) with the app's own `/livez`, so a hostname pointing at another service isn't reported as working. The check never follows redirects and resolves names with Go's own DNS client: a lookup made seconds too early otherwise stays in macOS's negative cache (seen with a real quick tunnel). It runs after connecting, first after 5 seconds, then every 5 seconds for a minute, and on demand.

What refuses requests through the tunnel:

| Surface | Check that holds through a tunnel | Test |
|---|---|---|
| The Dev Portal (`/_portal/…`) | Not tunnelled (another port); a named tunnel pointed at it by mistake still fails the portal's `Host` check (cloudflared forwards the public name) and the token | `TestGuardRefusesWhatItMust` (existing) |
| The dev console (`/_dev/*`) | `Host` (cloudflared forwards the public hostname) **and, new, forwarding headers**: a request with `Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, `X-Real-IP`, `True-Client-IP`, `CF-Connecting-IP`, `CF-Ray` or `CDN-Loop` is refused (403) before the token is looked at | `TestConsoleRefusesTunnelledRequests`: public and local `Host` with Cloudflare's headers and the token; each header alone; a direct request still works; app routes pass |
| The dev operator on `/ops/` | The same checks; a tunnelled request with the console token continues as anonymous, also behind `APP_TRUSTED_PROXIES` trusting loopback (which makes the visitor the peer) | `TestOperatorRefusesTunnelledRequests` |

Why the header check: `Host` alone is not enough. A named tunnel's public hostname has an *HTTP Host Header* setting in the Cloudflare dashboard; set to `localhost:8080`, every tunnelled request passes the `Host` and loopback checks, leaving the console token as the only barrier. cloudflared always adds `CF-Connecting-IP`, `CF-Ray`, `CDN-Loop` and `X-Forwarded-For`. Legitimate local callers send none of them: the portal's proxy uses `httputil.ReverseProxy.Rewrite`, which drops the `X-Forwarded-*` headers and adds none. A developer's own local reverse proxy in front of `/_dev/` that adds such headers is now refused; that is the price, and the console is a development tool reached through `orb dev`.

A real quick tunnel was checked by hand on 2026-09-17 (cloudflared 2026.3.0): `/livez` answered 200 through it, `/_dev/` and `/_dev/config` with the console token answered 403, `Ctrl+C` in `orb dev` left no cloudflared behind, and the portal's restart and stop replaced and ended the process.

### 5. Lifecycle

- `orb dev` owns cloudflared: it starts in its own process group (so a terminal's `Ctrl+C` reaches only `orb`), and stopping sends `SIGTERM` to the group, then `SIGKILL` after 5 seconds. The group is also killed when cloudflared exits by itself, so nothing it started lingers. On Linux cloudflared also gets `SIGTERM` if `orb dev` dies (`Pdeathsig`); on macOS a `SIGKILL` of `orb dev` itself can't be followed (known gap). Windows kills the process.
- `orb dev --tunnel quick|named` starts it after the portal; the portal starts, stops and restarts it; every way out of `orb dev` (return, `Ctrl+C`, `SIGTERM`) stops it before the portal.
- When the app starts on another address, a quick tunnel restarts with the new target; a named tunnel can't be retargeted from here, so the status says where to point its hostname (deviation from "restart it if the app port changes": a restart would change nothing).
- A tunnel that fails is not restarted automatically (no restart loops); the status keeps the failure and cloudflared's last 100 lines until the next start or stop.
- Status changes stream as `tunnel` events on `/_portal/api/events` (the latest is replayed to a new subscriber while a tunnel runs).
- Tunnels start only when `APP_ENV` is development (empty counts as development, as everywhere in `orb dev`), because a tunnel puts the app on the internet.

### 6. Configuration help

`GET /_portal/api/tunnel/setup` proposes `.env` changes for the connected URL, each with its current value, the proposed value and why; the screen shows them as a diff with checkboxes and applies the ticked ones with the env editor's `PUT env` (plan → diff → apply), after which `orb dev` rebuilds and restarts the app because `.env` changed.

| Key | Proposal |
|---|---|
| `APP_PUBLIC_URL` | The tunnel's origin |
| `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS` | The hostname and the origin (localhost origins can't be listed with another relying party) |
| `AUTH_DEFAULT_RETURN_TO` | Only when it points at this machine: the same path through the tunnel when it's the app's port, else the tunnel's `/docs` |
| `APP_CORS_ORIGINS` | Only when already set: the list plus the tunnel's origin, unticked by default (optional) |
| `APP_TRUSTED_PROXIES` | Adds `127.0.0.1/32` and `::1/128` unless loopback is covered, so rate limits, logs and audit events see the visitor's address |

The provider addresses are derived from the routes the running app serves (its `/openapi.json`): Google's redirect URI (`/v1/auth/google/callback`), Apple's return URL and server notifications, GitHub's callback URL and Resend's webhook, each with where to paste it and whether the app's environment configures the provider. Without the app's document (the app is stopped), gorbital's default paths are listed and the screen says so. A quick tunnel's setup warns that callbacks and passkeys made on it break with the next URL.

### 7. CLI output

`orb dev` prints a banner line and a few `orb:` lines (starting, the URL, connected, reachable, stopped, cloudflared's errors). `orb dev` has no JSON mode: its output is a live stream for people. The machine-readable surface is the portal's API (`GET /_portal/api/tunnel` and the `tunnel` events). Exit status: 2 for a wrong `--tunnel` value or `--tunnel-hostname` without `--tunnel named`, 1 when the tunnel can't start (cloudflared missing, `APP_ENV` not development, token or hostname missing or invalid).

## Endpoints

| Endpoint | Answer |
|---|---|
| `GET /_portal/api/tunnel` | `status`, `cloudflared` (found, path, version), `install`, `os`, `hostname` and its source, `token_source` or `token_problem` (never the token), `app_env`, `allowed`, `target` |
| `POST /_portal/api/tunnel/start` `{"mode","hostname"}` | 202 with the info; 400 `invalid_tunnel_mode` or `invalid_json`; 422 `cloudflared_missing`, `not_development`, `tunnel_token_missing`, `tunnel_token_invalid`, `tunnel_hostname_missing`, `tunnel_hostname_invalid`, `no_app_address` |
| `POST /_portal/api/tunnel/stop`, `…/restart` | 202 with the info; restart before any start is 409 `tunnel_not_connected` |
| `POST /_portal/api/tunnel/check` | The reachability result; 409 `tunnel_not_connected` |
| `GET /_portal/api/tunnel/setup` | Changes, `set`, callbacks, warnings; 409 without a URL |
| `PUT /_portal/api/tunnel/settings` `{"hostname"}` | The normalized hostname, saved; 422 when refused |

All behind the portal's guard; the writes need `X-Orb-Portal`.

## Threat model

| Threat | Mitigation |
|---|---|
| The dev console or dev operator reached from the internet through the tunnel | `Host` check, the new forwarding-header check, the token; tests simulate tunnelled requests with public and local `Host` |
| The Dev Portal reached through a named tunnel pointed at its port | The portal's `Host` and token checks; the portal is never a tunnel target |
| The token leaks through the process list, logs, the portal or the app | `TUNNEL_TOKEN` in cloudflared's environment only; redacted from output; the API reports only the variable's name; removed from the app's environment; tests with a fake cloudflared printing the token |
| A tunnel left running after `orb dev` | Process group killed on every exit path orb handles; tests check cloudflared and a grandchild are gone after stop, close, timeout and a crash; `Pdeathsig` on Linux |
| The app exposed in production by mistake | Tunnels refuse to start unless `APP_ENV` is development |
| Sign-in callbacks and passkeys bound to a URL that changes | Quick tunnels are labelled unstable in the status, the setup and the provider list |
| A hostname the check reports as working that isn't this app | `/livez` compared with the app's own; redirects not followed |
| `APP_TRUSTED_PROXIES` trusting loopback lets local processes choose their client address | Development only, proposed with its reason; the dev console and operator refuse forwarded requests regardless |
| The app itself is public while the tunnel runs (sign-in, public routes, what any account can do) | Stated on the screen at all times, in the setup warnings, the terminal and the guide; stopping is one click |

## Known gaps

- A `SIGKILL` of `orb dev` on macOS leaves cloudflared running (no parent-death signal there).
- A named tunnel's target lives in the Cloudflare dashboard; orb can only verify it by comparing `/livez`.
- The proposal doesn't offer to revert `.env` when the tunnel stops.
- No Dev Portal screenshots for the Tunnel screen yet.
- "Test now" for sign-in, the second half of Phase 11, is [ADR-0087](0087-testing-sign-in-from-the-dev-portal.md).
