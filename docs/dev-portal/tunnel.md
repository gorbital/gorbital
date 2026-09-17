# Tunnel

Put the app you run with `orb dev` on a public HTTPS address, with one click: receive webhooks, try the API from a phone, and test Google, Apple and GitHub sign-in and passkeys on a real domain. The tunnel is your own [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/); `orb dev` starts it, watches it and stops it with itself. Only the app goes through it.

> **While a tunnel runs, the app is on the internet.** Anyone with the URL reaches its sign-in, its public routes and whatever an account they can create is allowed to do. The dev console (`/_dev`), the development operator on `/ops` and the Dev Portal refuse requests that come through the tunnel, but your app's own routes don't. Stop the tunnel when you're done, and don't tunnel a database with data you care about.

## Two kinds of tunnel

| | Quick | Named |
|---|---|---|
| Address | A random `https://<words>.trycloudflare.com`, new on every start | Your hostname, such as `https://dev-api.example.com`, the same every time |
| Needs | cloudflared | cloudflared, a Cloudflare account with a domain, a tunnel created in the dashboard, its token |
| Good for | Webhooks (Resend, payment providers), the API from a phone, showing someone a build | Google, Apple and GitHub sign-in, passkeys, anything you register once with a provider |
| Not for | Sign-in callbacks and passkeys: they're bound to a URL that is gone next run | |

## Install cloudflared

orb runs the `cloudflared` on your `PATH` and never downloads it. When it's missing, the Tunnel screen and `orb dev --tunnel` show these steps for your system.

| System | Install |
|---|---|
| macOS | `brew install cloudflared` |
| Debian, Ubuntu | Cloudflare's apt repository, [pkg.cloudflare.com](https://pkg.cloudflare.com/index.html) |
| Fedora, RHEL, CentOS | Cloudflare's rpm repository, [pkg.cloudflare.com](https://pkg.cloudflare.com/index.html) |
| Any Linux | The release binary from [github.com/cloudflare/cloudflared/releases](https://github.com/cloudflare/cloudflared/releases/latest) |
| Windows | `winget install --id Cloudflare.cloudflared` |

Every package is listed on [Cloudflare's downloads page](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/). To use another binary, set `ORB_CLOUDFLARED` (a path, or a program name on `PATH`) in `.env` or the shell.

## A quick tunnel

From the portal: **Tunnel** in the sidebar (Bench), keep **Quick**, **Start quick tunnel**. From the terminal:

```sh
orb dev --tunnel quick
```

```text
  ✓ Tunnel     a quick tunnel to the app starts next; its URL follows (docs/dev-portal/tunnel.md)
orb: starting a quick tunnel to http://127.0.0.1:8080 (cloudflared; the URL follows)
orb: tunnel https://calm-river-demo-words.trycloudflare.com → http://127.0.0.1:8080 (quick: the URL changes on every start)
orb: tunnel connected: https://calm-river-demo-words.trycloudflare.com is on the internet while it runs (/_dev and the Dev Portal are not)
orb: tunnel reachable: https://calm-river-demo-words.trycloudflare.com/livez answered like the app
```

The tunnel forwards to the app's `APP_ADDR`. If the app starts on another port, the quick tunnel restarts with it (and gets a new URL).

## A named tunnel

### In the Cloudflare dashboard (once)

1. Add your domain to Cloudflare if it isn't there (its DNS must be on Cloudflare).
2. Open **Zero Trust** → **Networks** → **Tunnels** → **Create a tunnel**, choose **Cloudflared**, and name it (for example `my-app-dev`).
3. The next page shows install commands containing `--token eyJ…`. Copy the long value after `--token`: that's the tunnel's token. You don't need to run the command; `orb dev` runs cloudflared for you.
4. Add a public hostname (the dashboard calls it **Public Hostname** or **Published application route**): a subdomain such as `dev-api` on your domain, service type **HTTP**, URL `localhost:8080` (the app's port from `APP_ADDR`; the Tunnel screen shows the exact value). Leave **HTTP Host Header** empty.
5. Save. Cloudflare creates the DNS record.

Keep one tunnel and one hostname per developer: two people running the same tunnel share its traffic.

### In the app

Put the token in `.env` (it's a secret, like your other keys; `.env` isn't committed):

```sh
CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoi…
# or keep it in a file:
# CLOUDFLARE_TUNNEL_TOKEN_FILE=/Users/me/.secrets/my-app-dev-tunnel
ORB_TUNNEL_HOSTNAME=dev-api.example.com
```

The hostname can also be typed on the Tunnel screen (saved in `.orb/portal/tunnel.json`) or passed with `--tunnel-hostname`. Then start it from the screen (**Named**, **Start named tunnel**) or with:

```sh
orb dev --tunnel named
```

orb checks the token looks like a tunnel token and the hostname like a public name before starting. The token reaches cloudflared in its environment only (`TUNNEL_TOKEN`), never on its command line, never in the app's environment, never in anything the portal answers or prints: the screen shows which variable is set, not the value. When cloudflared connects, orb requests `https://<hostname>/livez` and compares the answer with the app's own `/livez`; "answered, but not like the app" means the public hostname points somewhere else.

## The screen

| Part | What it shows and does |
|---|---|
| The banner | What a tunnel exposes, always visible |
| Install cloudflared | When it's missing: the commands for this system (with Copy), other platforms on demand |
| Tunnel | Quick or Named; for named, the hostname (read-only when `ORB_TUNNEL_HOSTNAME` sets it) and whether the token is set; Start, Stop, Restart; the equivalent `orb dev --tunnel` |
| Status | State (off, starting, connected, stopping, failed), the public URL with Copy and a "stable" or "changes every run" badge, where it forwards, cloudflared's process, the last reachability check with **Check now**, a failure's reason, and cloudflared's output (secrets redacted) |
| .env for this address | The changes the address needs, as a diff per key with the reason; required ones ticked, optional ones not. **Apply** writes the ticked ones with the env editor, and `orb dev` rebuilds and restarts the app because `.env` changed |
| Register with providers | The exact URLs for this hostname, from the routes the app serves, with where to paste each |

The status also arrives live as `tunnel` events, so the sidebar's Tunnel entry shows a dot while a tunnel is connected.

### The .env changes

| Key | Proposed | Why |
|---|---|---|
| `APP_PUBLIC_URL` | `https://<hostname>` | Providers send people back to `APP_PUBLIC_URL/v1/auth/<provider>/callback`; emails link to it |
| `WEBAUTHN_RP_ID` | `<hostname>` | Passkeys belong to a domain |
| `WEBAUTHN_ORIGINS` | `https://<hostname>` | The origin that uses passkeys must match the relying party; localhost origins can't be listed with it |
| `AUTH_DEFAULT_RETURN_TO` | Only when it points at this machine: the same page through the tunnel (the app's port) or the tunnel's `/docs` | A phone or a provider's redirect can't reach `localhost` |
| `APP_CORS_ORIGINS` | Only when already set: add the tunnel's origin (optional) | For pages served through the tunnel that call the API |
| `APP_TRUSTED_PROXIES` | Add `127.0.0.1/32,::1/128` | cloudflared connects from this machine and sends the visitor's address in `X-Forwarded-For`; trusting loopback gives rate limits, logs and audit events the visitor's address |

Passkeys created on a tunnel's hostname work only there: put `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS` back (empty means `localhost` in development) to use passkeys locally again. The screen doesn't revert `.env` when the tunnel stops.

### What to register with providers

For `https://dev-api.example.com` (paths as gorbital's sign-in and mail events modules serve them):

| Provider | Where | Value |
|---|---|---|
| Google | Google Cloud Console → Google Auth Platform → Clients → your Web application client → Authorized redirect URIs | `https://dev-api.example.com/v1/auth/google/callback` |
| Apple | Apple Developer → Identifiers → your Services ID → Sign in with Apple → Configure: Domains and Subdomains, Return URLs | `dev-api.example.com`, `https://dev-api.example.com/v1/auth/apple/callback` |
| Apple (notifications) | Apple Developer → Identifiers → your App ID → Sign in with Apple → Server-to-Server Notification Endpoint | `https://dev-api.example.com/v1/auth/apple/notifications` |
| GitHub | Settings → Developer settings → OAuth Apps → an app for this hostname → Authorization callback URL (one per app) | `https://dev-api.example.com/v1/auth/github/callback` |
| Resend | Webhooks → Add endpoint, events `email.bounced` and `email.complained`; its signing secret goes in `RESEND_WEBHOOK_SECRET` | `https://dev-api.example.com/v1/webhooks/resend` |

The provider guide's [Testing on a real domain](../guides/auth-providers.md#testing-on-a-real-domain) has the rest of each provider's setup.

## Security

- **Only the app is tunnelled.** The quick tunnel's target is `APP_ADDR`; the portal (port 3100) never is. A named tunnel pointed at the portal by mistake is refused by its `Host` and token checks.
- **The dev console and the development operator refuse tunnelled requests** even with the token: cloudflared sends the public hostname as `Host`, and adds `CF-Connecting-IP`, `CF-Ray`, `CDN-Loop` and `X-Forwarded-For`, which the console refuses in any case (so an **HTTP Host Header** of `localhost` set in the dashboard doesn't open it). A local reverse proxy of your own in front of `/_dev/` that adds forwarding headers is refused too.
- **Development only.** With `APP_ENV` other than development, the tunnel doesn't start.
- **No orphans.** cloudflared runs in its own process group; `orb dev` stops the group when you stop the tunnel, when cloudflared exits, and when `orb dev` ends (return, `Ctrl+C`, `SIGTERM`). On Linux it also dies if `orb dev` is killed; on macOS, `kill -9` of `orb dev` itself leaves it running.
- **The token** is never logged, printed, returned or copied: see [A named tunnel](#a-named-tunnel).

Decided in [ADR-0086](../adr/0086-dev-portal-tunnel.md), with the threat model.

## Then test sign-in

With the tunnel's `.env` changes applied, [Testing sign-in](testing-sign-in.md) checks Google, Apple and GitHub against the tunnel's callback URLs and runs a passkey ceremony on its hostname, without creating accounts.

## Flags, variables and endpoints

| Flag or variable | Effect |
|---|---|
| `orb dev --tunnel quick` | Start a quick tunnel with the app |
| `orb dev --tunnel named` | Start the named tunnel; `--tunnel-hostname dev-api.example.com` sets its hostname |
| `CLOUDFLARE_TUNNEL_TOKEN`, `CLOUDFLARE_TUNNEL_TOKEN_FILE` | The named tunnel's token, or a file holding it (not both) |
| `ORB_TUNNEL_HOSTNAME` | The named tunnel's hostname |
| `ORB_CLOUDFLARED` | The cloudflared to run instead of the one on `PATH` |

Exit status 2 for a wrong `--tunnel` value, 1 when the tunnel can't start. Endpoints: `GET /_portal/api/tunnel`, `POST …/start`, `…/stop`, `…/restart`, `…/check`, `GET …/setup`, `PUT …/settings` ([Dev Portal guide](../guides/dev-portal.md#tunnel)).

## Troubleshooting

| You see | Do |
|---|---|
| `cloudflared isn't installed` | Install it (above), or set `ORB_CLOUDFLARED` |
| `APP_ENV is production: orb dev starts a tunnel only for development` | Run the tunnel with a development `.env` |
| `a named tunnel needs its token` / `this isn't a Cloudflare tunnel token` | Copy only the value after `--token` from the dashboard's install command |
| `cloudflared didn't report a trycloudflare.com URL within 45s` | Your network may block cloudflared's outbound connections (UDP 7844 for QUIC, or TCP 443); cloudflared's output on the screen has its errors. A `~/.cloudflared/config.yml` from another tunnel can also get in a quick tunnel's way: rename it while testing |
| `cloudflared exited` | Its last error follows; for a named tunnel, a wrong or revoked token is the usual cause |
| Not reachable yet: `no such host` | A new name takes a few seconds to resolve; orb retries for a minute, then **Check now** |
| `Cloudflare answered 530` or `502` | cloudflared isn't reaching the app: is the app running, and does the named tunnel's public hostname point at the app's port? |
| `answered, but not like the app's /livez` | The public hostname points at another service |
| Google says `redirect_uri_mismatch` | Register the callback URL exactly as listed, and apply `APP_PUBLIC_URL` |
