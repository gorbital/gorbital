# Sign-in provider setup

What a developer provides so each sign-in method works with their own accounts, where to find every value and where to paste it. Decisions: [ADR-0045](../adr/0045-sign-in-provider-setup.md), [ADR-0043](../adr/0043-two-factor-authentication.md) (authenticator apps), [ADR-0044](../adr/0044-passkeys.md) (passkeys), [ADR-0046](../adr/0046-google-and-apple-sign-in.md) (Google and Apple), [ADR-0059](../adr/0059-github-sign-in.md) (GitHub).

**The step-by-step guide** lives in every Full app as [`AUTH_PROVIDERS.md`](../../examples/full-single/AUTH_PROVIDERS.md), next to `AGENTS.md`: creating an authenticator encryption key, choosing a passkey relying party, finding Apple Team and Bundle IDs and Android signing fingerprints, creating Google Cloud OAuth clients, creating Apple App IDs, Services IDs and `.p8` keys, relaying email through Apple, creating GitHub OAuth apps, email provider keys, common errors, and a production checklist. `.env.example` has a commented block per method.

## How the app tells you what's missing

| Where | What you see |
|---|---|
| App start in development (`orb dev`), v0.1 apps | A **Sign-in methods** block: `✓` for each method that's on, `–` with the variables to set and the guide section for each that's off |
| `go run ./cmd/api auth-providers` | The same block, anywhere (CI, a production shell), in v0.1 apps and apps on `gorbital.Main` |
| `GET /ops/auth/providers` | The same status as JSON for dashboards, with permission `ops.auth.read`; never values. In an app on `gorbital.Main`, with `opshttp.Module()` beside `authhttp` |
| App start in production; in an app on `gorbital.Main`, in every environment | One info log line per method: `sign-in method`, with `method` and `configured` |
| A half-filled method | The app refuses to start and names the variable to fix (exit code 2 on `gorbital.Main`) |

```text
Sign-in methods
  ✓ Email and password
  ✓ Authenticator apps (2FA)
  ✓ Passkeys in browsers      RP ID localhost; origins http://localhost:8080, http://localhost:3000
  – Passkeys in iOS apps      set WEBAUTHN_APPLE_APP_IDS in .env  AUTH_PROVIDERS.md#passkeys-in-ios-apps
  – Passkeys in Android apps  set WEBAUTHN_ANDROID_APPS in .env   AUTH_PROVIDERS.md#passkeys-in-android-apps
```

## What to provide

| Method | Variable | Secret | Where to get it |
|---|---|---|---|
| Authenticator apps | `AUTH_ENCRYPTION_KEYS` | **Yes** | `echo "k1:$(openssl rand -base64 32)"`; `orb dev` writes one in development; required in production |
| Passkeys | `WEBAUTHN_RP_ID` | No | Your site's domain, such as `example.com`; empty in development means `localhost` |
| Passkeys | `WEBAUTHN_ORIGINS` | No | Browser origins of your frontend, such as `https://app.example.com`; https in production |
| Passkeys in iOS apps | `WEBAUTHN_APPLE_APP_IDS` | No | `TEAMID.bundle.id`: Apple Developer → Membership details → Team ID, plus the app's bundle ID |
| Passkeys in Android apps | `WEBAUTHN_ANDROID_APPS` | No | `package.name=SHA256:FINGERPRINT`: `applicationId`, and Play Console → App integrity → App signing key certificate → SHA-256 (or `keytool` for debug keys) |
| Google, Apple and GitHub | `APP_PUBLIC_URL` | No | The API's public URL, which providers return to; empty in development means `http://localhost:8080`; https in production |
| Google, Apple and GitHub | `AUTH_DEFAULT_RETURN_TO` | No | A page of your frontend where browser sign-ins without `return_to` end; empty in development means the API docs; required in production |
| Google sign-in | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` (secret), `GOOGLE_IOS_CLIENT_ID`, `GOOGLE_ANDROID_CLIENT_ID` | Secret: yes | Google Cloud Console → Google Auth Platform → Clients; redirect URI `https://<API>/v1/auth/google/callback` |
| Apple sign-in | `APPLE_TEAM_ID`, `APPLE_SERVICES_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_FILE` (secret), `APPLE_BUNDLE_IDS` | Key: yes | Apple Developer → Identifiers (App ID, Services ID) and Keys (Sign in with Apple, `.p8`); return URL `https://<API>/v1/auth/apple/callback`; notifications `https://<API>/v1/auth/apple/notifications` |
| GitHub sign-in | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` (secret) | Secret: yes | GitHub → Settings → Developer settings → OAuth Apps → New OAuth App (one per environment); callback URL `https://<API>/v1/auth/github/callback` |
| Email | `RESEND_API_KEY` or `SMTP_*` | **Yes** | Resend → API Keys, or your SMTP provider; `orb add mail` |

Files the backend serves for native apps, generated from the variables (nothing to upload): `/.well-known/apple-app-site-association` and `/.well-known/assetlinks.json`. They must be reachable on `WEBAUTHN_RP_ID`'s domain; when a separate web frontend serves that domain, it proxies the two paths to the API.

## Testing on a real domain

`localhost` is enough for email, authenticator apps and passkeys in a browser on your machine. Google, Apple and GitHub sign-in from a phone, Apple's web sign-in (which refuses `http` return URLs), passkeys on a real domain and provider webhooks need the app on a public HTTPS address. In development, `orb dev` gives it one with a [tunnel](../dev-portal/tunnel.md) ([ADR-0086](../adr/0086-dev-portal-tunnel.md)):

1. Create a **named** tunnel in the Cloudflare dashboard with a hostname such as `dev-api.example.com` pointing at the app's port, and put its token in `.env` as `CLOUDFLARE_TUNNEL_TOKEN`. A quick tunnel (`*.trycloudflare.com`) works for webhooks and a phone, but its URL changes on every run, so callbacks registered with it and passkeys created on it break the next day.
2. Start it: the Dev Portal's **Tunnel** screen, or `orb dev --tunnel named --tunnel-hostname dev-api.example.com`.
3. Apply the `.env` changes the screen proposes: `APP_PUBLIC_URL=https://dev-api.example.com`, `WEBAUTHN_RP_ID=dev-api.example.com`, `WEBAUTHN_ORIGINS=https://dev-api.example.com`, `AUTH_DEFAULT_RETURN_TO` when it pointed at `localhost`, and `APP_TRUSTED_PROXIES` for loopback so rate limits see the visitor's address. `orb dev` restarts the app.
4. Register the addresses the screen lists for that hostname (use a separate OAuth client or app for development, so production's settings stay untouched):

| Provider | Register | Value |
|---|---|---|
| Google | Web application client → Authorized redirect URIs | `https://dev-api.example.com/v1/auth/google/callback` |
| Apple | Services ID → Sign in with Apple → Domains and Subdomains; Return URLs | `dev-api.example.com`; `https://dev-api.example.com/v1/auth/apple/callback` |
| Apple | App ID → Server-to-Server Notification Endpoint | `https://dev-api.example.com/v1/auth/apple/notifications` |
| GitHub | An OAuth App for this hostname → Authorization callback URL | `https://dev-api.example.com/v1/auth/github/callback` |
| Resend (email events) | Webhooks → Add endpoint (`email.bounced`, `email.complained`); secret in `RESEND_WEBHOOK_SECRET` | `https://dev-api.example.com/v1/webhooks/resend` |

5. Open `https://dev-api.example.com/docs` on the phone or laptop and sign in.

While the tunnel runs, the app is reachable by anyone with the address: its sign-in and public routes included. The dev console and the development operator refuse tunnelled requests; stop the tunnel when you're done. Passkeys created on the tunnel's hostname work only there; set `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS` back to empty to use `localhost` again.
## In an app on gorbital.Main

Providers are configured the same way: by the variables above, never in code. `authhttp.New` has no option for Google, Apple, GitHub or passkeys, so `auth-providers`, `/ops/auth/providers` and `.env` always agree; a method is off while its variables are empty ([configuring sign-in](configuring-sign-in.md#what-stays-in-environment-variables)).

Two options change what a provider sign-in does:

| Option | Effect on Google, Apple and GitHub |
|---|---|
| `authhttp.WithoutRegistration()` | A first sign-in of an address without an account gets 403 `registration_closed` (`#error=registration_closed` in the web flow), and no account is created. Existing accounts still sign in and link providers |
| `authhttp.OnRegister(hook)` | Runs when a first sign-in creates an account, with `Method` `google`, `apple` or `github` and the name the provider gave; its refusal answers 403 with the app's code ([sign-in hooks](sign-in-hooks.md#onregister)) |

## Later stages

The generated `AUTH_PROVIDERS.md` ends with checklists for when the web frontend, iOS app and Android app are built: which endpoints to call, the Associated Domains entitlement, Credential Manager, Sign in with Apple and Google SDK setup, and where to store tokens. The backend already provides everything those clients need.
