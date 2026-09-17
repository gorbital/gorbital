# Email guide

How a Full preset app sends email: choosing Resend or SMTP, where each setting lives, development with Mailpit, sending from code and fixing delivery problems. Implemented in `examples/full-single`. Decisions: [ADR-0025](../adr/0025-email-providers.md), [ADR-0037](../adr/0037-email-setup-and-delivery.md), [ADR-0062](../adr/0062-resend-webhooks-and-suppression-list.md).

## How email flows

```text
module code ─► mailer ─────────────► job queue ─► mail worker ─────────► Mailpit (development)
               fills the sender from   (retries,    skips suppressed      or Resend / SMTP
               mail.* settings          idempotent)  addresses
```

- Code calls `mailer.Send(ctx, mail.Message{...})`. The message is validated and stored as a job, so the request doesn't wait for the provider.
- The mail worker delivers it. Temporary failures (rate limits, outages) retry up to 8 times with the same idempotency key, so nobody gets the email twice. Permanent refusals (an unverified domain, an invalid address) stop at once and show in the job run.
- Before each send, the worker drops recipients on the [suppression list](#bounces-complaints-and-the-suppression-list): addresses that bounced permanently or marked an email as spam. An email left with no recipient is cancelled, not retried.

## Choose a provider

Run this inside the app:

```bash
orb add mail
```

```text
┃ How should the app send email?
┃ Switch any time by running orb add mail again. In development, email always lands in the Dev Portal's inbox.
┃ > Resend: an email API, the quickest to set up (recommended)
┃   SMTP: Amazon SES, Postmark, Mailgun, Google Workspace or your own server
```

`orb` then asks only what that provider needs, shows a summary to confirm, and prints the steps that remain. Secrets you type are hidden and saved only in `.env`, which git ignores.

| Want | Command |
|---|---|
| Resend, asked step by step | `orb add mail` |
| Resend, no questions | `orb add mail --provider resend --yes` (add the key to `.env` yourself) |
| SMTP with flags | `orb add mail --smtp-host smtp.postmarkapp.com --smtp-port 587 --smtp-username <token>` (asks for the password) |
| See what would change | `orb add mail --provider smtp --dry-run` |
| Switch provider | Run `orb add mail` again and pick the other one; it replaces `internal/app/infra_mail.go` and its tests, `infra_mail_test.go` |

All flags are in the [CLI guide](cli.md#orb-add-mail).

## Where each setting lives

| Setting | Where | Change it |
|---|---|---|
| Resend API key | `.env`: `RESEND_API_KEY` | Edit `.env`, restart |
| Resend webhook signing secret | `.env`: `RESEND_WEBHOOK_SECRET` (optional) | Edit `.env`, restart |
| SMTP server and login | `.env`: `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Edit `.env`, restart |
| The dev inbox, Mailpit or real email | `.env`: `MAIL_DELIVERY` (`devmail`, `mailpit` or `provider`; empty means the Dev Portal's inbox in development, provider in production) | Edit `.env`, restart |
| Sender name | Runtime setting `mail.from_name` (default: the app name) | `PUT /ops/settings/mail.from_name`, live |
| Sender address | Runtime setting `mail.from_email` (default: `no-reply@example.com`) | `PUT /ops/settings/mail.from_email`, live |
| Reply-to address | Runtime setting `mail.reply_to` (default: none) | `PUT /ops/settings/mail.reply_to`, live |

Secrets are never runtime settings, and the sender is never an environment variable.

## Set up Resend

1. Create an API key at [resend.com/api-keys](https://resend.com/api-keys) ("Sending access" is enough).
2. Put it in `.env`, or paste it when `orb add mail` asks:
   ```bash
   RESEND_API_KEY=re_…
   ```
3. Verify the domain you send from at [resend.com/domains](https://resend.com/domains).
4. Set the sender (next section) to an address on that domain.

## Set up SMTP

| Provider | `SMTP_HOST` | `SMTP_PORT` / `SMTP_TLS` | Username and password |
|---|---|---|---|
| Amazon SES | `email-smtp.<region>.amazonaws.com` | 587 / `starttls` | SES SMTP credentials (not your AWS keys) |
| Postmark | `smtp.postmarkapp.com` | 587 / `starttls` | The server API token, as both |
| Mailgun | `smtp.mailgun.org` (`smtp.eu.mailgun.org` in the EU) | 587 / `starttls` | The domain's SMTP login and password |
| Google Workspace or Gmail | `smtp.gmail.com` | 587 / `starttls` | Your address and an app password |
| Your own server | its host name | 465 / `tls`, or 587 / `starttls` | as configured |

`SMTP_TLS=none` is only for servers on your own machine; `orb` refuses to send a password unencrypted to another host.

## Set the sender

Start the app, sign in as an account with `platform_admin` and keep the token in `$TOKEN` ([authentication guide](authentication.md#your-first-administrator)), then change the sender through the admin API. It applies to the next email on every instance, without a restart:

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/mail.from_email \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"hello@yourdomain.com","version":0,"reason":"our verified domain"}'

curl -X PUT http://127.0.0.1:8080/ops/settings/mail.from_name \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"Your App","version":0,"reason":"our name"}'
```

The sender settings (`mail.from_email`, `mail.from_name`, `mail.reply_to`) need a reason, kept in their history: they decide who password reset and invitation emails appear to come from and where replies go. Send `version` from `GET /ops/settings/mail.from_email` when the setting was changed before. In production, the app logs a warning at startup while the sender is still `no-reply@example.com`.

## Send a test email

```bash
curl http://127.0.0.1:8080/ops/mail -H "Authorization: Bearer $TOKEN"
```

```json
{"provider": "resend", "delivery": "mailpit", "details": {"api_key": "missing", "webhook_secret": "missing"}, "from_name": "Your App", "from_email": "hello@yourdomain.com"}
```

```bash
curl -X POST http://127.0.0.1:8080/ops/mail/test \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to":"you@example.com"}'
```

It returns 202 once the email is queued. See the delivery in `GET /ops/jobs/runs?kind=gorbital.mail.send`.

## Bounces, complaints and the suppression list

Sending again to an address that doesn't exist, or to someone who marked your email as spam, hurts your sender reputation until providers deliver your password resets to spam folders or suspend your account. The app keeps a **suppression list** in PostgreSQL and never emails an address on it:

| Event from the provider | What happens |
|---|---|
| Hard bounce (Resend `email.bounced` with type `Permanent`) | The recipient is suppressed |
| Complaint (`email.complained`: marked as spam) | The recipient is suppressed |
| Soft bounce (`Transient`, such as a full mailbox) or `Undetermined` | Nothing: a later email may arrive |
| Any other event (`email.delivered`, `email.opened`, …) | Accepted and ignored |

- Emails to a suppressed address are cancelled when the worker picks them up, and the run says `every recipient is on the suppression list` (without the address). Other recipients of the same email still receive it.
- The list works with both providers. Only Resend reports bounces to the app today; with SMTP, the list is empty unless you add to it from your own code with `suppressionpg.Store.Add`.
- Addresses are personal data. They are stored trimmed and in lower case, shown only to operators with `ops.mail.read`, left out of audit events and logs, and kept until an operator removes them: a hard bounce stays true until the mailbox exists again.

### Connect Resend's webhook

Your API must be reachable from the internet over https (in development, use a tunnel such as `cloudflared tunnel --url http://127.0.0.1:8080`). A v0.1 app serves the webhook from `internal/modules/mailevents`; an app on `gorbital.Main` adds `gorbital.WithModules(mailevents.Module())` in `main.go` ([Methods](../methods/gorbital-mailevents.md)), which refuses to start with a malformed secret.

1. Open [resend.com/webhooks](https://resend.com/webhooks) and choose **Add Webhook**.
2. Endpoint URL: `https://<your API>/v1/webhooks/resend`.
3. Events: select **email.bounced** and **email.complained** (other events are accepted but ignored, so leave them off to save requests).
4. Save, open the webhook and copy its **Signing Secret**, which starts with `whsec_`.
5. Put it in `.env` (or your platform's secret store) on every instance, and restart:
   ```bash
   RESEND_WEBHOOK_SECRET=whsec_…
   ```
   A secret that isn't `whsec_` and base64 stops the app at startup with a message naming the variable.
6. Check `GET /ops/mail` shows `"webhook_secret": "configured"`.
7. Test it with real delivery (`MAIL_DELIVERY=provider`, always the case in production): send an email to `bounced@resend.dev` (Resend's bounce simulator; `complained@resend.dev` simulates a complaint) with `POST /ops/mail/test`, then `GET /ops/mail/suppressions` lists the address within a few seconds, and `GET /ops/audit?action=mail.suppression.added` shows the event. Remove it again as below.

How the endpoint protects itself:

- Every request must carry Resend's Svix signature (`svix-id`, `svix-timestamp`, `svix-signature`): an HMAC-SHA256 of the ID, timestamp and raw body with the signing secret, compared in constant time. Several signatures are accepted while Resend rotates the secret. Anything else answers 401 `invalid_webhook_signature`.
- A request signed more than 5 minutes before or after the server's clock is refused, and each applied delivery's `svix-id` is remembered for 10 minutes, so a captured request can't be replayed, for example to suppress an address an operator just removed. Resend's retries of a failed delivery use the same ID and are applied once.
- Bodies are limited to 256 KiB. No session or cookie is involved; cross-origin protection lets Resend's server-to-server requests through (they have no `Origin`) and still refuses browsers posting from other sites.
- Without `RESEND_WEBHOOK_SECRET`, or in an SMTP app, the endpoint answers 404 `webhook_not_found`.
- During maintenance mode it answers 503 like the rest of the API, and Resend retries later.

### Review and remove suppressions

```bash
curl http://127.0.0.1:8080/ops/mail/suppressions -H "Authorization: Bearer $TOKEN"
```

```json
{"suppressions": [{"id": 12, "email": "ada@example.com", "reason": "bounce", "source": "resend", "detail": "Permanent/General",
  "created_at": "2026-09-16T10:00:00Z", "updated_at": "2026-09-16T10:00:00Z"}], "next_cursor": "12"}
```

Filter with `?reason=bounce` or `?reason=complaint`; pages hold up to 100 (`limit`, `cursor`). When an address works again (the person fixed their mailbox, or asked for email after a complaint), remove it with a reason, which is recorded in the audit event `mail.suppression.removed`. Don't put the address in the reason:

```bash
curl -X DELETE http://127.0.0.1:8080/ops/mail/suppressions/12 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"mailbox exists again (support ticket 4821)"}'
```

Listing needs `ops.mail.read` (`platform_admin`, `ops_viewer`); removing needs `ops.mail.write` (`platform_admin`). The address is suppressed again at its next hard bounce or complaint.

## Development: the inbox in the Dev Portal

With `MAIL_DELIVERY` empty (or `devmail`), every email the app sends, whatever the provider, goes to `orb dev`'s mail catcher ([ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)): an SMTP server `orb dev` runs on `DEV_MAIL_SMTP_ADDR` (default `127.0.0.1:1025`), whose inbox is the Dev Portal's **Mail** screen (http://127.0.0.1:3100/mail): the message as HTML, text or source, verification codes with a copy button, links, attachments. Messages are kept under `.orb/portal/mail` (the last 500), so they survive the app's restarts; nobody real is emailed. Without the portal (`orb dev --no-portal`) the catcher doesn't run: use Mailpit or the provider.

The Mail screen also renders the app's **email previews** with sample data and sends one to the inbox: the auth module's messages (`auth.BrandedEmailPreviews`), in multi-tenant apps the organisation invitation, and the test message, all in the app's [email layout](#email-templates). A v0.1 app lists them in `internal/app/mail_previews.go`, where you add your own; an app on `gorbital.Main` gets them from its modules, which register them during `Setup` with `gorbital.AuthSetup.MailPreviews`, as `authhttp` does for sign-in's messages. The dev console serves them at `GET /_dev/mail/previews`, `GET /_dev/mail/preview?name=` and `POST /_dev/mail/preview/send?name=&to=`.

Prefer Mailpit? Run it yourself (or keep the `mailpit` service an older `compose.yaml` has), set `MAIL_DELIVERY=mailpit` and `MAILPIT_SMTP_ADDR`; `/_dev/mail` then proxies its inbox. To try the real provider while developing, set `MAIL_DELIVERY=provider` in `.env` with its credentials, and restart.

## Send email from code

Modules receive the mailer (a `mail.Sender`) as a dependency. Leave `From` empty to use the `mail.*` settings. Render the message with the app's brand, so it looks like every other email the app sends ([ADR-0078](../adr/0078-branded-email-layout.md)):

```go
msg := a.brand().Message(user.Email, "Your invoice is ready", "invoice", mail.Email{
	Preheader:  "Invoice 1042 for March",
	Title:      "Your invoice is ready",
	Paragraphs: []string{"Invoice 1042 for March is ready to download."},
	Button:     mail.Button{Label: "Open invoice", URL: invoiceURL},
	Closing:    []string{"Questions about a charge? Reply to this email."},
})
err := mailer.Send(ctx, msg)
```

- `Send` returns once the email is queued; a validation error (no recipient, a line break in the subject) returns at once.
- Set `IdempotencyKey` (for example `"welcome-" + user.ID`) when the same email could be queued twice.
- Tags reach Resend (letters, digits, `_` and `-`); SMTP ignores them. `Message` sets the `category` tag from its third argument.
- Inside a transaction, enqueue through the jobs client's `InsertTx` so the email is sent only if the transaction commits ([background jobs](background-jobs.md)).
- A `mail.Message` with your own `Text` and `HTML` still works; the layout is a convenience, not a requirement.

## Email templates

Every email the app sends shares one layout, rendered by `mail.Brand` from the core `mail` package: the wordmark (or your logo) at the top, one card with the message, a footer with the name, the link and a support address. Emails are light, one 560px column of tables with inline styles, so they read the same in Gmail, Outlook, Apple Mail and the Dev Portal's preview.

The brand is built once, in `internal/app/mail.go`:

```go
func (a *App) brand() mail.Brand {
	return mail.Brand{
		Name:         ServiceName,
		URL:          a.cfg.Social.PublicURL,
		LogoURL:      "https://cdn.example.com/acme-logo.png", // shown instead of the name, 32px high
		SupportEmail: "help@example.com",
		Footer:       "Acme Ltd, 1 Orbit Way, London",
	}
}
```

The auth module's emails (`authlib.NewBrandedEmails`), the organisation invitation (`orgslib.NewBrandedEmails`) and the test message take it from there. What each email can hold is a `mail.Email`:

| Field | Shown as |
|---|---|
| `Preheader` | The line inbox lists show after the subject; hidden in the opened email |
| `Title` | The heading |
| `Paragraphs` | The message |
| `Code`, `CodeLabel`, `CodeNote` | A one-time code, large in a dark block, with its caption and the expiry line under it |
| `Button` | The main action as a lime button, with the URL repeated as a link |
| `Closing` | Smaller lines at the end: "If you didn't do this…" |

`Brand.Render` returns the HTML and a plain-text alternative built from the same content; `Brand.Message` wraps both in a `mail.Message` with the `category` tag. Only `http`, `https` and `mailto` links are rendered; anything else is dropped. Check the result in the Dev Portal's Mail screen (**Previews**), which renders every email with sample data.

To change the look entirely, implement the modules' `Emails` interfaces with your own templates and pass them in `internal/app/app.go` instead of `NewBrandedEmails`.

## Troubleshooting

| Symptom | Where to look | Fix |
|---|---|---|
| App won't start: `RESEND_API_KEY is required` | Startup error | Add the key to `.env`, or leave `MAIL_DELIVERY` empty in development |
| App won't start: `SMTP_HOST is required` | Startup error | Add the SMTP variables to `.env` |
| Nothing in Mailpit | `docker compose ps`; `MAILPIT_SMTP_ADDR` | Start Mailpit; check the address matches `compose.yaml` |
| Run cancelled with `403 … domain is not verified` | `GET /ops/jobs/runs?kind=gorbital.mail.send` | Verify the domain in Resend, or change `mail.from_email` |
| Run retrying with `401 … check RESEND_API_KEY` | Job runs | Fix the key in `.env` and restart; pending retries then succeed |
| Run cancelled with `550` | Job runs | The SMTP server refused the sender or recipient; check the address and your provider's sending rules |
| Run cancelled with `every recipient is on the suppression list` | Job runs; `GET /ops/mail/suppressions` | The address bounced or complained before; remove it only if it works again |
| Resend shows webhook failures with 401 | Resend's webhook log; app logs `email webhook refused` | `RESEND_WEBHOOK_SECRET` doesn't match the webhook's signing secret, or the server clock is more than 5 minutes off |
| Resend shows webhook failures with 404 | Resend's webhook log | Set `RESEND_WEBHOOK_SECRET` and restart; check the URL ends in `/v1/webhooks/resend` |
| Run retrying with `authenticate … check the SMTP username and password` | Job runs | Fix `SMTP_USERNAME` / `SMTP_PASSWORD` and restart |

Retried and cancelled runs keep their error messages; message contents and recipients are never shown by the ops APIs. Providers' replies often quote the recipient, so the SMTP and Resend senders and the mail worker replace email addresses in error text with `[email]` before it reaches the run or the logs. `POST /ops/mail/test` allows 5 test emails an hour per operator.

## Environment reference

| Variable | Provider | Default | Notes |
|---|---|---|---|
| `MAIL_DELIVERY` | both | `devmail` (development), `provider` (production) | `devmail` and `mailpit` are refused in production |
| `DEV_MAIL_SMTP_ADDR` | both | `127.0.0.1:1025` | Where `orb dev`'s mail catcher listens ([ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)) |
| `MAILPIT_SMTP_ADDR` | both | `127.0.0.1:1025` | A Mailpit you run, with `MAIL_DELIVERY=mailpit` |
| `RESEND_API_KEY` | Resend | none | Required when delivery is `provider` |
| `RESEND_WEBHOOK_SECRET` | Resend | none | The webhook's signing secret (`whsec_…`); empty turns `POST /v1/webhooks/resend` off |
| `SMTP_HOST` | SMTP | none | Required when delivery is `provider` |
| `SMTP_PORT` | SMTP | `587` | |
| `SMTP_TLS` | SMTP | `starttls` | `starttls`, `tls` or `none` |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP | none | The password is required with a username |
