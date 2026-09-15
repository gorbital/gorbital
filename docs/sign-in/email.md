# Email sending

Your app sends email for sign-up codes, password reset codes and security alerts, such as "a passkey was added to your account". On your computer, nothing leaves your machine: every email goes to a local inbox called Mailpit. In production, the app sends real email through a provider: **Resend** or any **SMTP** server.

| Where | Where email goes | What you set |
|---|---|---|
| Your computer | Mailpit, at http://127.0.0.1:8025 | Nothing |
| Your computer, trying the real provider | The provider | `MAIL_DELIVERY=provider`, plus the provider's values |
| Production | The provider, always | The provider's values |

## What Mailpit is

Mailpit is a small program that pretends to be an email server. Your app hands it emails exactly as it would hand them to a real provider, and Mailpit keeps them in an inbox you open in your browser, instead of delivering them.

- **Why you need it:** to sign up on your own app you need the 6-digit code from the verification email. Mailpit shows it to you without a real email account, and you can't email a real person by mistake.
- **Where it comes from:** `aps dev` starts it in Docker from your app's `compose.yaml`, next to PostgreSQL (image `axllent/mailpit`).
- **Where to see it:** http://127.0.0.1:8025. The app sends to it on port 1025.
- **If you removed it:** in development, emails would fail to send and stay queued for retries; nobody could finish sign-up locally.
- **In production:** never. The app refuses to start with `MAIL_DELIVERY=mailpit` when `APP_ENV=production`.

> [!NOTE]
> No key is needed for Mailpit. It has no password and only listens on your own computer (`127.0.0.1`).

## Option 1: Resend (recommended)

[Resend](https://resend.com) is an email service with a simple API. You need one value from it: an API key.

| Variable | Secret? | Looks like | Where it comes from |
|---|---|---|---|
| `RESEND_API_KEY` | **Yes** | `re_123abc…` | Copied from the Resend dashboard, shown once |

<div class="steps">

1. **Create an account**

   Go to [resend.com](https://resend.com) and sign up. The free plan is enough to start.

2. **Add your sending domain**

   In the dashboard, open **Domains** → **Add Domain**. Enter the domain your emails come from, such as `example.com` (or a subdomain such as `mail.example.com`, which keeps your main domain's reputation separate). Choose the region closest to your users.

3. **Add the DNS records**

   Resend shows a few records: SPF and DKIM, which prove to inboxes that Resend may send for your domain, and an MX record for bounces. Open the website where you bought your domain (your DNS host, such as Cloudflare, Namecheap or Route 53) and add each record exactly as shown: type, name and value.

   Back in Resend, click **Verify DNS Records**. It can take from a few minutes to a few hours. Wait for **Verified**.

4. **Create the API key**

   Open **API Keys** → **Create API Key**. Name it after the environment, such as `acme-api production`. Set **Permission** to **Sending access** and **Domain** to the domain from step 2, so the key can only send, and only from that domain. Click **Add**.

5. **Copy the key**

   The key is shown **once**. Copy it straight into your secret store (or `.env` to test locally). If you lose it, delete the key in Resend and create another.

   ```bash
   RESEND_API_KEY=re_123abc…
   ```

</div>

New Full apps already use Resend. If you switched to SMTP earlier, switch back with `aps add mail --provider resend`.

## Option 2: SMTP

SMTP is the standard way to send email, supported by every provider: Amazon SES, Postmark, Mailgun, Google Workspace or your own mail server. Switch your app to it once:

```bash
aps add mail --provider smtp
```

`aps` asks for each value, hides the password as you type it, and saves secrets only in `.env`. Run it inside your app, with no uncommitted changes. It changes `internal/app/infra_mail.go`, the email block of `.env.example`, `apistock.yaml` and `go.mod`; commit the result.

| Variable | Required? | Secret? | Example | What it is |
|---|---|---|---|---|
| `SMTP_HOST` | Yes, when sending real email | No | `smtp.postmarkapp.com` | Your provider's SMTP server name |
| `SMTP_PORT` | No (default `587`) | No | `587` | The server's port |
| `SMTP_TLS` | No (default `starttls`) | No | `starttls` | How the connection is encrypted: `starttls` for 587 and 2525, `tls` for 465, `none` only for a server on your own machine |
| `SMTP_USERNAME` | Depends on the provider | No | `apikey` | The SMTP login |
| `SMTP_PASSWORD` | Yes, with a username | **Yes** | | The SMTP password or token |

Where each provider shows these values:

| Provider | `SMTP_HOST` | Username and password |
|---|---|---|
| Amazon SES | `email-smtp.<region>.amazonaws.com`, such as `email-smtp.eu-west-1.amazonaws.com` | SES console → **SMTP settings** → **Create SMTP credentials**. These are special SMTP credentials, not your AWS access keys |
| Postmark | `smtp.postmarkapp.com` | Your server → **API Tokens** → the Server API token, used as both username and password |
| Mailgun | `smtp.mailgun.org`, or `smtp.eu.mailgun.org` in the EU | **Sending** → **Domain settings** → **SMTP credentials** |
| Google Workspace or Gmail | `smtp.gmail.com` | Your address, and an app password from your Google Account's security settings |

Whatever the provider, verify your sending domain there first, as with Resend.

## Set the sender address

The sender's name and address aren't environment variables: they're **runtime settings**, which an administrator changes while the app runs.

| Setting | Default | Change it to |
|---|---|---|
| `mail.from_email` | `no-reply@example.com` | An address on your verified domain, such as `hello@example.com` |
| `mail.from_name` | Your app's name | What people see as the sender, such as `Acme` |
| `mail.reply_to` | Empty | Where replies go, such as `support@example.com` |

Sign in as an administrator ([how](../start/quickstart.md#6-sign-in-as-the-administrator)), keep the token in `$TOKEN`, then:

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/mail.from_email \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value": "hello@example.com", "version": 0, "reason": "our domain is verified"}'
```

`version` is `0` the first time. If the setting was changed before, read its current `version` with `GET /ops/settings/mail.from_email` first: the app refuses a stale version, so two administrators can't overwrite each other by accident.

## Check it works

1. See what the app will use:

   ```bash
   curl http://127.0.0.1:8080/ops/mail -H "Authorization: Bearer $TOKEN"
   ```

   ```json
   {"provider": "resend", "delivery": "mailpit", "details": {"api_key": "missing"}, "from_name": "acme-api", "from_email": "no-reply@example.com"}
   ```

   `delivery` is `mailpit` or `provider`. `details` says whether the key or SMTP login is set, never its value.

2. Send yourself a test email:

   ```bash
   curl -X POST http://127.0.0.1:8080/ops/mail/test \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"to": "you@example.com"}'
   ```

   The answer is `202` with `{"status": "queued", …}`: the email is queued, and a background job delivers it within seconds. In development, open http://127.0.0.1:8025 to see it.

3. If it doesn't arrive, look at the delivery job:

   ```bash
   curl "http://127.0.0.1:8080/ops/jobs/runs?kind=apistock.mail.send" -H "Authorization: Bearer $TOKEN"
   ```

## If something goes wrong

| What you see | What it means | Fix |
|---|---|---|
| The app won't start: `RESEND_API_KEY is required to send email with Resend` | Delivery is `provider` (always in production) and the key is empty | Set `RESEND_API_KEY`, or in development leave `MAIL_DELIVERY` empty |
| The app won't start: `MAIL_DELIVERY=mailpit is for development` | Production can't use Mailpit | Remove `MAIL_DELIVERY` or set it to `provider` |
| Nothing in Mailpit | Mailpit isn't running, or the app sends to another port | `docker compose ps` should show `mailpit` as healthy; `MAILPIT_SMTP_ADDR` must match `MAILPIT_SMTP_PORT` |
| Job run cancelled with `403 … domain is not verified` | Resend refuses your sender address | Finish domain verification, or set `mail.from_email` to an address on a verified domain |
| Job run retrying with `401 … check RESEND_API_KEY` | Wrong or deleted key | Put the right key in the environment and restart; waiting retries then succeed |
| Job run retrying with `authenticate … check the SMTP username and password` | Wrong SMTP login | Fix `SMTP_USERNAME` and `SMTP_PASSWORD`, restart |
| Job run cancelled with `550` | The SMTP server refused the sender or recipient | Check the address and your provider's sending rules |
| People who signed in with Apple and hid their email get nothing | Apple only relays from registered senders | Register your domain with Apple ([Apple sign-in, step 6](apple.md#step-6-let-apple-forward-emails)) |

Temporary failures retry up to 8 times, and each email is sent at most once even when retried. Permanent refusals stop straight away, so you see the reason instead of waiting for retries.
