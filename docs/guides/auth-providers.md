# Sign-in provider setup

What a developer provides so each sign-in method works with their own accounts, where to find every value and where to paste it. Decisions: [ADR-0045](../adr/0045-sign-in-provider-setup.md), [ADR-0043](../adr/0043-two-factor-authentication.md) (authenticator apps), [ADR-0044](../adr/0044-passkeys.md) (passkeys).

Every Full app ships this guide as `AUTH_PROVIDERS.md` next to `AGENTS.md`, and a commented block per method in `.env.example`.

## How the app tells you what's missing

| Where | What you see |
|---|---|
| App start in development (`aps dev`) | A **Sign-in methods** block: `✓` for each method that's on, `–` with the variables to set and the guide section for each that's off |
| `go run ./cmd/api auth-providers` | The same block, anywhere (CI, a production shell) |
| `GET /ops/auth/providers` | The same status as JSON for dashboards, with permission `ops.auth.read`; never values |
| App start in production | One log line per method: `method`, `configured` |
| A half-filled method | The app refuses to start and names the variable to fix |

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
| Authenticator apps | `AUTH_ENCRYPTION_KEYS` | **Yes** | `echo "k1:$(openssl rand -base64 32)"`; `aps dev` writes one in development; required in production |
| Passkeys | `WEBAUTHN_RP_ID` | No | Your site's domain, such as `example.com`; empty in development means `localhost` |
| Passkeys | `WEBAUTHN_ORIGINS` | No | Browser origins of your frontend, such as `https://app.example.com`; https in production |
| Passkeys in iOS apps | `WEBAUTHN_APPLE_APP_IDS` | No | `TEAMID.bundle.id`: Apple Developer → Membership details → Team ID, plus the app's bundle ID |
| Passkeys in Android apps | `WEBAUTHN_ANDROID_APPS` | No | `package.name=SHA256:FINGERPRINT`: `applicationId`, and Play Console → App integrity → App signing key certificate → SHA-256 (or `keytool` for debug keys) |
| Google sign-in (later) | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` (secret), `GOOGLE_IOS_CLIENT_ID`, `GOOGLE_ANDROID_CLIENT_ID` | Secret: yes | Google Cloud Console → APIs & Services → Credentials → OAuth client IDs; redirect URI `https://<API>/v1/auth/google/callback` |
| Apple sign-in (later) | `APPLE_TEAM_ID`, `APPLE_SERVICES_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_FILE` (secret), `APPLE_BUNDLE_IDS` | Key: yes | Apple Developer → Identifiers (Services ID) and Keys (Sign in with Apple, `.p8`); return URL `https://<API>/v1/auth/apple/callback` |

Files the backend serves for native apps, generated from the variables (nothing to upload): `/.well-known/apple-app-site-association` and `/.well-known/assetlinks.json`. They must be reachable on `WEBAUTHN_RP_ID`'s domain; when a separate web frontend serves that domain, it proxies the two paths to the API.

## Later stages

The generated `AUTH_PROVIDERS.md` ends with checklists for when the web frontend, iOS app and Android app are built: which endpoints to call, the Associated Domains entitlement, Credential Manager, and where to store tokens. The backend already provides everything those clients need.
