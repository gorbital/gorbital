# Go-live checklist

Work through this list before real people use your app. Each line names the value to check and where it's explained. Everything here was set up on your computer with development values; production needs its own.

> [!DONT]
> Never copy your `.env` file to a server. Development values are weak on purpose (a public database password, a throwaway encryption key) and some are refused in production. Create every production value fresh, in your hosting provider's secret settings.

## 1. The basics

- [ ] `APP_ENV=production`. It turns on JSON logs and HSTS, refuses Mailpit, turns docs off by default, and makes the settings below required. The Docker image sets it for you; without `APP_ENV` the app refuses to start.
- [ ] `APP_ADDR=0.0.0.0:8080` when the app runs in a container behind a load balancer. The Docker image sets it for you. The default `127.0.0.1:8080` only accepts connections from the same machine.
- [ ] `DATABASE_URL` points at your production PostgreSQL, with `sslmode=require` (or stricter) and a strong password. Store it as a secret, or mount it as a file and set `DATABASE_URL_FILE`.
- [ ] Migrations run **before** each new version starts: `docker run --entrypoint /migrate <image>` with the same environment, or `go run ./cmd/migrate`. The app never migrates itself.
- [ ] `APP_CORS_ORIGINS` lists your web frontends, such as `https://app.example.com`, all https. Empty means browsers on other sites can't call the API.
- [ ] Decide on `APP_DOCS_ENABLED`. `/docs` and `/openapi.json` are off in production; set it to `true` if your API reference is meant to be public.

## 2. Encryption key

- [ ] A new `AUTH_ENCRYPTION_KEYS`, not the one from your `.env`. Without it the app refuses to start in production. [How to generate it](encryption-key.md).
- [ ] A copy of the key saved in your password manager.

## 3. Email

- [ ] An email provider: `RESEND_API_KEY` for Resend, or `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME` and `SMTP_PASSWORD` for SMTP. [Email sending](email.md).
- [ ] Your sending domain verified at the provider (SPF and DKIM records added).
- [ ] The sender set with `PUT /ops/settings/mail.from_email` to an address on that domain. The default `no-reply@example.com` logs a warning in production and most providers refuse it.
- [ ] A test email received: `POST /ops/mail/test` with `{"to": "you@yourdomain.com"}`.

## 4. Passkeys

- [ ] `WEBAUTHN_RP_ID` set to your domain, such as `example.com`. Empty turns passkeys off in production. [Passkeys](passkeys.md).
- [ ] `WEBAUTHN_ORIGINS` lists your https frontends, and the same addresses are in `APP_CORS_ORIGINS`.
- [ ] If you have mobile apps: `WEBAUTHN_APPLE_APP_IDS` and `WEBAUTHN_ANDROID_APPS` set, and `https://<your domain>/.well-known/apple-app-site-association` and `/.well-known/assetlinks.json` reachable. [Passkeys in mobile apps](passkeys-mobile.md).

## 5. Google, Apple and GitHub

- [ ] `APP_PUBLIC_URL` is your API's https address, such as `https://api.example.com`, if you use any of them.
- [ ] `AUTH_DEFAULT_RETURN_TO` is a page of your website that reads `#error=…`, such as `https://app.example.com/signed-in`, if you use any of them on the web: the app won't start without it in production. [After signing in](github.md#after-signing-in).
- [ ] Google: production redirect URI `https://api.example.com/v1/auth/google/callback` added to the web client; consent screen **published** (in Testing, only listed test users can sign in); `GOOGLE_CLIENT_SECRET` stored as a secret. [Google sign-in](google.md).
- [ ] Apple: production domain and return URL `https://api.example.com/v1/auth/apple/callback` on the Services ID; notification endpoint `https://api.example.com/v1/auth/apple/notifications` on the App ID; the `.p8` key stored as a secret file; your sending domain registered for Apple's private email relay. [Apple sign-in](apple.md).
- [ ] GitHub: a production OAuth app whose callback URL is `https://api.example.com/v1/auth/github/callback`; `GITHUB_CLIENT_SECRET` stored as a secret. [GitHub sign-in](github.md).

## 6. Your first administrator

Seed data only exists in development, in apps created with `orb` v0.1: `cmd/seed` refuses to run in production, and an app on `gorbital.Main` has none. Create the first administrator by hand:

1. Sign up through your frontend or `POST /v1/auth/register`, and verify the email.
2. In the production environment, run `/api grant-role you@example.com platform_admin` (or `go run ./cmd/api grant-role …` from a machine with the production environment).
3. Sign in and turn on an authenticator app (`POST /v1/auth/mfa/totp`, then `POST /v1/auth/mfa/totp/confirm`). `/ops` answers 403 `mfa_required` until you do.

## 7. Behind a load balancer or proxy

- [ ] The load balancer checks `GET /readyz` (ready to receive traffic) and `GET /livez` (the process is alive).
- [ ] `APP_TRUSTED_PROXIES` lists your load balancers' address ranges, such as `10.0.0.0/8`. Sign-in requests are rate-limited per client IP address (60 a minute, `auth.ip_requests_per_minute`); without this setting every request seems to come from the load balancer, so all users share one limit and logs show the balancer's address. Don't add your own `X-Forwarded-For` middleware. [Running in production](../guides/production.md#tls-proxies-and-client-ips).
- [ ] `APP_TRUSTED_CALLERS` stays empty unless a gateway or internal service sets request IDs or traces you want kept.
- [ ] Rate limits are stored in PostgreSQL and shared by every instance, so adding instances doesn't raise them. Raise the `auth.*` limit settings through `/ops/settings` if real traffic needs it.
- [ ] Deploys allow about 30 seconds for a clean stop: the app waits 5 seconds for the load balancer to notice, then up to 25 seconds for requests and jobs to finish.

## 8. Final check

In the production environment, run:

```bash
/api auth-providers
```

Every sign-in method you expect shows `✓`. Then sign in as a normal user and as the administrator from your real frontend.
