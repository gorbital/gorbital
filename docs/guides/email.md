# Email guide

How a Full preset app sends email: choosing Resend or SMTP, where each setting lives, development with Mailpit, sending from code and fixing delivery problems. Implemented in `examples/full-single`. Decisions: [ADR-0025](../adr/0025-email-providers.md), [ADR-0037](../adr/0037-email-setup-and-delivery.md).

## How email flows

```text
module code ─► mailer ─────────────► job queue ─► mail worker ─► Mailpit (development)
               fills the sender from   (retries,                  or Resend / SMTP
               mail.* settings          idempotent)
```

- Code calls `mailer.Send(ctx, mail.Message{...})`. The message is validated and stored as a job, so the request doesn't wait for the provider.
- The mail worker delivers it. Temporary failures (rate limits, outages) retry up to 8 times with the same idempotency key, so nobody gets the email twice. Permanent refusals (an unverified domain, an invalid address) stop at once and show in the job run.

## Choose a provider

Run this inside the app:

```bash
aps add mail
```

```text
┃ How should the app send email?
┃ Switch any time by running aps add mail again. In development, email always lands in Mailpit.
┃ > Resend: an email API, the quickest to set up (recommended)
┃   SMTP: Amazon SES, Postmark, Mailgun, Google Workspace or your own server
```

`aps` then asks only what that provider needs, shows a summary to confirm, and prints the steps that remain. Secrets you type are hidden and saved only in `.env`, which git ignores.

| Want | Command |
|---|---|
| Resend, asked step by step | `aps add mail` |
| Resend, no questions | `aps add mail --provider resend --yes` (add the key to `.env` yourself) |
| SMTP with flags | `aps add mail --smtp-host smtp.postmarkapp.com --smtp-port 587 --smtp-username <token>` (asks for the password) |
| See what would change | `aps add mail --provider smtp --dry-run` |
| Switch provider | Run `aps add mail` again and pick the other one |

All flags are in the [CLI guide](cli.md#aps-add-mail).

## Where each setting lives

| Setting | Where | Change it |
|---|---|---|
| Resend API key | `.env`: `RESEND_API_KEY` | Edit `.env`, restart |
| SMTP server and login | `.env`: `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Edit `.env`, restart |
| Mailpit or real email | `.env`: `MAIL_DELIVERY` (`mailpit` or `provider`; empty means Mailpit in development, provider in production) | Edit `.env`, restart |
| Sender name | Runtime setting `mail.from_name` (default: the app name) | `PUT /ops/settings/mail.from_name`, live |
| Sender address | Runtime setting `mail.from_email` (default: `no-reply@example.com`) | `PUT /ops/settings/mail.from_email`, live |
| Reply-to address | Runtime setting `mail.reply_to` (default: none) | `PUT /ops/settings/mail.reply_to`, live |

Secrets are never runtime settings, and the sender is never an environment variable.

## Set up Resend

1. Create an API key at [resend.com/api-keys](https://resend.com/api-keys) ("Sending access" is enough).
2. Put it in `.env`, or paste it when `aps add mail` asks:
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

`SMTP_TLS=none` is only for servers on your own machine; `aps` refuses to send a password unencrypted to another host.

## Set the sender

Start the app, sign in as an account with `platform_admin` and keep the token in `$TOKEN` ([authentication guide](authentication.md#your-first-administrator)), then change the sender through the admin API. It applies to the next email on every instance, without a restart:

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/mail.from_email \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"hello@yourdomain.com","version":0}'

curl -X PUT http://127.0.0.1:8080/ops/settings/mail.from_name \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"Your App","version":0}'
```

Send `version` from `GET /ops/settings/mail.from_email` when the setting was changed before. In production, the app logs a warning at startup while the sender is still `no-reply@example.com`.

## Send a test email

```bash
curl http://127.0.0.1:8080/ops/mail -H "Authorization: Bearer $TOKEN"
```

```json
{"provider": "resend", "delivery": "mailpit", "details": {"api_key": "missing"}, "from_name": "Your App", "from_email": "hello@yourdomain.com"}
```

```bash
curl -X POST http://127.0.0.1:8080/ops/mail/test \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to":"you@example.com"}'
```

It returns 202 once the email is queued. See the delivery in `GET /ops/jobs/runs?kind=apistock.mail.send`.

## Development: Mailpit

`docker compose up -d --wait` starts Mailpit next to PostgreSQL. With `MAIL_DELIVERY` empty, every email the app sends, whatever the provider, lands in the inbox at **http://127.0.0.1:8025**, and nobody real is emailed.

To try the real provider while developing, set `MAIL_DELIVERY=provider` in `.env` with its credentials, and restart.

## Send email from code

Modules receive the mailer (a `mail.Sender`) as a dependency. Leave `From` empty to use the `mail.*` settings:

```go
err := mailer.Send(ctx, mail.Message{
	To:      []mail.Address{{Email: user.Email}},
	Subject: "Verify your email",
	Text:    "Your code is " + code,
	HTML:    "<p>Your code is <strong>" + code + "</strong></p>",
	Tags:    map[string]string{"category": "verification"},
})
```

- `Send` returns once the email is queued; a validation error (no recipient, a line break in the subject) returns at once.
- Set `IdempotencyKey` (for example `"welcome-" + user.ID`) when the same email could be queued twice.
- Tags reach Resend (letters, digits, `_` and `-`); SMTP ignores them.
- Inside a transaction, enqueue through the jobs client's `InsertTx` so the email is sent only if the transaction commits ([background jobs](background-jobs.md)).

## Troubleshooting

| Symptom | Where to look | Fix |
|---|---|---|
| App won't start: `RESEND_API_KEY is required` | Startup error | Add the key to `.env`, or leave `MAIL_DELIVERY` empty in development |
| App won't start: `SMTP_HOST is required` | Startup error | Add the SMTP variables to `.env` |
| Nothing in Mailpit | `docker compose ps`; `MAILPIT_SMTP_ADDR` | Start Mailpit; check the address matches `compose.yaml` |
| Run cancelled with `403 … domain is not verified` | `GET /ops/jobs/runs?kind=apistock.mail.send` | Verify the domain in Resend, or change `mail.from_email` |
| Run retrying with `401 … check RESEND_API_KEY` | Job runs | Fix the key in `.env` and restart; pending retries then succeed |
| Run cancelled with `550` | Job runs | The SMTP server refused the sender or recipient; check the address and your provider's sending rules |
| Run retrying with `authenticate … check the SMTP username and password` | Job runs | Fix `SMTP_USERNAME` / `SMTP_PASSWORD` and restart |

Retried and cancelled runs keep their error messages; message contents and recipients are never shown by the ops APIs.

## Environment reference

| Variable | Provider | Default | Notes |
|---|---|---|---|
| `MAIL_DELIVERY` | both | `mailpit` (development), `provider` (production) | `mailpit` is refused in production |
| `MAILPIT_SMTP_ADDR` | both | `127.0.0.1:1025` | Mailpit's SMTP address |
| `MAILPIT_SMTP_PORT`, `MAILPIT_WEB_PORT` | both | `1025`, `8025` | Host ports in `compose.yaml` |
| `RESEND_API_KEY` | Resend | none | Required when delivery is `provider` |
| `SMTP_HOST` | SMTP | none | Required when delivery is `provider` |
| `SMTP_PORT` | SMTP | `587` | |
| `SMTP_TLS` | SMTP | `starttls` | `starttls`, `tls` or `none` |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP | none | The password is required with a username |
