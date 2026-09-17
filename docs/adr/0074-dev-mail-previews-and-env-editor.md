# ADR-0074: Dev mail, email previews and the env editor

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0008, ADR-0025, ADR-0028, ADR-0065, ADR-0066 · **Amended by:** ADR-0078 (previews render in the branded layout; multi-tenant apps preview the invitation)

## Context

Phase 9 of the [Dev Portal roadmap](../dev-portal-roadmap.md): an inbox inside the portal (replacing Mailpit in the development `compose.yaml`), email templates rendered with sample data and sent to the inbox, an editor for `.env` that knows the app's configuration, and flags and settings from `/ops` (already served; the screen is the portal's).

Today a Full app in development sends email to Mailpit, a container from `compose.yaml` ([ADR-0025](0025-email-providers.md)); the dev console proxies its API at `/_dev/mail` ([ADR-0065](0065-dev-console.md)). Mailpit is good, but it is a second UI, a second port pair, a container to wait for, and the codes and links a developer needs sit behind another tab.

## Options

### The inbox

| Option | Verdict |
|---|---|
| Keep Mailpit and embed its UI in the portal | Rejected: an iframe of another product, another origin's cookies, and still a container |
| **An SMTP catcher inside `orb dev` (`cli/internal/devmail`): plain SMTP on `DEV_MAIL_SMTP_ADDR` (default `127.0.0.1:1025`, loopback only, no TLS, no authentication), messages kept under `.orb/portal/mail` (the raw `.eml`, a parsed summary, the envelope), bounded to 500, parsed for the screen: bodies, attachments, verification codes, links** | **Chosen**: nothing to start, the same lifetime as `orb dev`, the codes where the developer reads them |
| Which delivery the app uses | **`MAIL_DELIVERY=devmail`, the default in development (`mailpit` and `provider` stay: an app that keeps its Mailpit, or CI with `GORBITAL_TEST_MAILPIT_*`, needs no change). `orb dev` starts the catcher when the app's `.env` resolves to devmail and a Full app runs; Mailpit is gone from the compose template, and `orb dev` only waits for the services `compose.yaml` defines** |

### Email previews

| Option | Verdict |
|---|---|
| Templates as files with a preview renderer | Rejected: the auth module builds its messages in Go (paragraphs, escaped into HTML), and it should keep doing so |
| **Previews as code: `auth.EmailPreviews(appName)` builds every message of the module with sample data through a capturing sender; the app lists them (plus a plain test message) in `mailPreviews()`, and the dev console serves `GET /_dev/mail/previews`, `GET /_dev/mail/preview?name=&to=` and `POST /_dev/mail/preview/send?name=&to=`, the send going through the app's mailer, so it lands in the inbox like a real message. The console's first POST endpoint: an endpoint can now declare `post`** | **Chosen**: a preview is the real code path, so it can't drift from what users get |

### The env editor

| Option | Verdict |
|---|---|
| The app rewrites its own `.env` | Rejected: the app must not write its configuration, and it restarts to read it anyway |
| **`orb dev` edits `.env` (`cli/internal/portal/env.go`): every key of `.env` and `.env.example` with the example's comment as description, secrets masked by name until revealed, keys of the example missing from `.env` flagged, set/unset rewriting the file in place (comments, order and blank lines kept, new keys after the example's comment), a restart offered after a change. The dev console's `/_dev/config` says which keys the running app read and which are secret; the screen joins the two** | **Chosen**: the file is the source of truth; `orb dev` owns the developer's machine, the app doesn't |

Validation against the app's configuration schema is what the app does when it restarts: the portal shows the build or start error from the output, which names the variable.

## Decision

| Piece | Decision |
|---|---|
| App | `MAIL_DELIVERY` accepts `devmail` (the development default), `mailpit`, `provider`; `DEV_MAIL_SMTP_ADDR`; `mail_previews.go`, wired as `Sources.MailPreviews`; Mailpit removed from `compose.yaml` and `.env.example` |
| `devmail` | `Server` (SMTP, loopback only), `Store` (`.orb/portal/mail`, 500 messages, summaries with codes and snippet, details with text, HTML, links, attachments, headers, envelope), `Parse` |
| Portal | `GET /_portal/api/mail?q=&limit=`, `mail/stream` (SSE), `mail/{id}`, `mail/{id}/html` (sandboxed by CSP for an iframe), `mail/{id}/source`, `DELETE mail/{id}`, `DELETE mail`; `GET /_portal/api/env`, `GET env/{key}` (reveal), `PUT env` (`set`, `unset`; answers `restart_needed`) |
| Dev console | `MailPreviewer` and the three endpoints; the OpenAPI document |
| `orb dev` | Starts the catcher for devmail; the banner and the `mail` link point at the portal's Mail screen; `composeServicesIn` skips Mailpit when `compose.yaml` lacks it; the health check reports the catcher |
| The screens | Mail (inbox, HTML/text/source, codes with copy, links, delete, clear, previews with send), Environment (the editor), Flags and Settings (as before) |

## Why

- One tool: `orb dev` already runs the app; catching its email is a few hundred lines, not a container.
- Previews from the real builders can't lie; a template preview would.
- `.env` is the developer's file: editing it in place with its comments keeps it readable in an editor too.

## Trade-offs

- The catcher speaks the minimum SMTP; it refuses STARTTLS and AUTH. A sender configured for TLS (`MAIL_DELIVERY=provider` with SMTP) isn't for the catcher.
- Existing apps keep Mailpit until they remove it from `compose.yaml` and switch `MAIL_DELIVERY`; both work.
- The env editor doesn't know the app's schema by itself: an unknown key is accepted and the app decides on restart.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): the catcher is loopback only and holds whatever the app sent; `.env` values (secrets) travel from `orb dev` to the browser only on reveal, behind the portal's guard; the HTML view is sandboxed.
- Upgrade notes: new apps have no Mailpit; existing apps may drop it.
- Guides: [email](../guides/email.md), [environment variables](../guides/environment-variables.md), [Dev Portal](../guides/dev-portal.md), [dev console](../guides/dev-console.md), [CLI](../guides/cli.md), [local development](../guides/local-development.md).

## Implementation notes (2026-09-16)

`cli/internal/devmail` tests (parsing, the SMTP session through `net/smtp`, the store's bounds and reopen), `TestMailEndpoints` and `TestEnvEndpoints` in `cli/internal/portal`, `TestEmailPreviews` in `modules/auth`, the dev console's OpenAPI test.
