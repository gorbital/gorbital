# ADR-0045: Sign-in provider setup

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0028, ADR-0044

## Context

apistock provides passkeys, Google sign-in and Apple sign-in (v0.3), but it never owns the accounts they depend on. Each app's developer creates their own Apple Developer and Google Cloud resources and gives the app the identifiers, keys and fingerprints. Mobile apps and web frontends are built later; the backend must be ready for them now.

The maintainer's requirement (2026-09-15): developers must be told plainly what they need to provide, where to get it and where to paste it, in the `.env` file, in the documentation and in the terminal, so every sign-in method works once they have filled it in. ADR-0024 already promised that social providers are "disabled until credentials are configured" and that `aps dev` "reports their status"; ADR-0028's banner showed `Google login: not configured → docs/auth-providers.md`. Nothing defines what that report contains or where the instructions live.

## Decision

### 1. What the developer provides

Everything is an environment variable (ADR-0031: identifiers and keys of external accounts are infrastructure and secrets, not runtime settings). An empty variable means the method is off; the rest of the app works.

**Passkeys** ([ADR-0044](0044-passkeys.md))

| Variable | Secret | Where to get it | Example |
|---|---|---|---|
| `WEBAUTHN_RP_ID` | No | Your site's domain, the one users see; no account needed | `example.com` (development: `localhost`) |
| `WEBAUTHN_ORIGINS` | No | The browser origins of your web frontend and API docs | `https://app.example.com` |
| `WEBAUTHN_APPLE_APP_IDS` | No | Apple Developer → Membership details → **Team ID**, and your app's **Bundle ID** (Xcode target, or Certificates, Identifiers & Profiles → Identifiers) | `ABCDE12345.com.example.app` |
| `WEBAUTHN_ANDROID_APPS` | No | Your app's **package name** (`applicationId`) and the **SHA-256 fingerprint** of its signing certificate: Play Console → Test and release → App integrity → App signing key certificate, or `keytool -list -v -keystore <keystore>` for debug builds | `com.example.app=SHA256:AB:CD:…` |

Files the backend serves for native apps (no file to upload; generated from the variables above):

| Path | Needed by | Generated from |
|---|---|---|
| `/.well-known/apple-app-site-association` | iOS passkeys (the app's Associated Domains entitlement `webcredentials:<RP ID>`) | `WEBAUTHN_APPLE_APP_IDS` |
| `/.well-known/assetlinks.json` | Android passkeys (Credential Manager) | `WEBAUTHN_ANDROID_APPS` |

**Google sign-in** (implemented with its own ADR; variable names are fixed here)

| Variable | Secret | Where to get it |
|---|---|---|
| `GOOGLE_CLIENT_ID` | No | Google Cloud Console → APIs & Services → Credentials → Create credentials → OAuth client ID → **Web application**; first configure the OAuth consent screen |
| `GOOGLE_CLIENT_SECRET` | **Yes** (`GOOGLE_CLIENT_SECRET_FILE` also works) | Shown with the web client ID |
| `GOOGLE_IOS_CLIENT_ID` | No | Same page → OAuth client ID → **iOS**, with the bundle ID (for the native flow's ID tokens) |
| `GOOGLE_ANDROID_CLIENT_ID` | No | Same page → OAuth client ID → **Android**, with the package name and SHA-1 fingerprint |

To register in Google Cloud: authorized redirect URI `https://<your API>/v1/auth/google/callback` (development: `http://localhost:8080/v1/auth/google/callback`).

**Apple sign-in** (implemented with its own ADR; variable names are fixed here)

| Variable | Secret | Where to get it |
|---|---|---|
| `APPLE_TEAM_ID` | No | Apple Developer → Membership details → Team ID |
| `APPLE_SERVICES_ID` | No | Certificates, Identifiers & Profiles → Identifiers → **Services IDs** → new, with Sign in with Apple enabled (the web client ID) |
| `APPLE_KEY_ID` | No | Keys → new key with Sign in with Apple → Key ID |
| `APPLE_PRIVATE_KEY_FILE` | **Yes** (or `APPLE_PRIVATE_KEY` with the contents) | The `.p8` file downloaded once when the key is created; keep it outside the repository |
| `APPLE_BUNDLE_IDS` | No | Your iOS apps' bundle IDs, for the native flow's ID tokens |

To register at Apple: the Services ID's domain and return URL `https://<your API>/v1/auth/apple/callback`. Apple doesn't accept `localhost` for the web flow; test it through a tunnel or use the native flow (ADR-0024).

### 2. Where it is written

| Place | Content |
|---|---|
| `.env.example` | A commented block per method: each variable, whether it is a secret, where to get it in one line, the URLs to register, and a pointer to `AUTH_PROVIDERS.md`. Empty by default |
| `AUTH_PROVIDERS.md` in every Full app (next to `AGENTS.md`) | Step-by-step setup per method with the console paths above, what to paste where, the URLs to register, how to check it works, and a "when you build the web frontend / iOS app / Android app" checklist (entitlements, Credential Manager, callback handling) for later stages |
| `docs/guides/auth-providers.md` (apistock) | The same guide for the repository |
| `AGENTS.md` | One row: configure sign-in methods in `.env` following `AUTH_PROVIDERS.md`; never commit the values |

### 3. How the developer is told

| Where | Behaviour |
|---|---|
| App start, development | Prints a **Sign-in methods** block: each method `✓ on` or `– off`, and for each off method the exact variables to set and the `AUTH_PROVIDERS.md` section. `aps dev` shows it as the app starts |
| App start, production | Logs one line per method at info (`method`, `configured`), never values |
| Partial configuration | Fails at start in every environment, naming the missing variables (for example `GOOGLE_CLIENT_ID` without `GOOGLE_CLIENT_SECRET`), so a half-configured provider can't silently stay off |
| `go run ./cmd/api auth-providers` | Prints the same block and exits non-zero when a configured method is invalid, for CI and deploy checks |
| `GET /ops/auth/providers` | Each method's status and missing variable names, never values, for the future dashboard; permission `ops.auth.read` (in `ops_viewer` and `platform_admin`) |
| `aps new --preset full` | Next steps mention `AUTH_PROVIDERS.md` for passkeys, Google and Apple |

Example in development:

```text
Sign-in methods
  ✓ Email and password
  ✓ Two-factor authentication (authenticator apps)
  ✓ Passkeys in browsers          RP ID localhost, origins http://localhost:8080, http://localhost:3000
  – Passkeys in iOS apps          set WEBAUTHN_APPLE_APP_IDS in .env        AUTH_PROVIDERS.md#passkeys-in-ios-apps
  – Passkeys in Android apps      set WEBAUTHN_ANDROID_APPS in .env         AUTH_PROVIDERS.md#passkeys-in-android-apps
  – Google sign-in                set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET AUTH_PROVIDERS.md#google
  – Apple sign-in                 set APPLE_TEAM_ID, APPLE_SERVICES_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY_FILE  AUTH_PROVIDERS.md#apple
```

Methods appear in the block once they are implemented; Google and Apple join with their ADR.

### 4. Frontend and mobile, later

The backend ships everything a client needs now: the endpoints, the generated association files, and the provider callbacks when Google and Apple land. `AUTH_PROVIDERS.md` records what the web frontend and mobile apps must do when they are built (call `navigator.credentials` with the returned options, add the Associated Domains entitlement, handle the Google and Apple native SDK tokens), so those stages start from a checklist rather than a search.

### Secrets

Only `GOOGLE_CLIENT_SECRET` and the Apple private key are secrets: `config.Secret`, `*_FILE` supported, never printed, logged, returned by `/ops` or accepted as flags or runtime settings (threat 19).

## Why

- Developers can't guess console paths and fingerprint formats; one table per method, repeated where they look (`.env`, docs, terminal), removes the guesswork.
- A status block at start answers "why doesn't Google sign-in work?" before anyone asks.
- Failing on partial configuration catches the common mistake of pasting half the credentials.
- A status endpoint and command give the future dashboard and CI the same information without exposing values.

## Trade-offs

- Console paths change; the guide needs occasional updates.
- One more permission (`ops.auth.read`) and endpoint.
- Printing configuration status at every development start adds a few lines of output.

## Consequences

- ADR-0044 uses this for passkeys: its variables, generated files, status lines, `AUTH_PROVIDERS.md` sections and `.env.example` block ship with it.
- The Google and Apple ADR adopts the variable names and status behaviour above.
- The Full preset gains `AUTH_PROVIDERS.md`, the status block, `auth-providers` command and `GET /ops/auth/providers`.

## Implementation notes (2026-09-15)

- The status comes from one place, `Config.signInMethods` in `internal/app/providers.go`: method key and name, enabled, a detail for enabled methods (relying party ID and origins, app IDs, Android packages), and the missing variables and guide section for the others. The start block, the command and the endpoint all use it.
- Partial configuration is refused by `LoadConfig`: `WEBAUTHN_ORIGINS` or native app IDs without `WEBAUTHN_RP_ID`, an RP ID without origins, http origins in production, origins off the RP ID, and malformed Apple app IDs or Android fingerprints, each naming the variable and `AUTH_PROVIDERS.md`.
- `aps new --preset full` prints `AUTH_PROVIDERS.md` in its next steps, and the docs URL on `localhost`, which passkeys need.
- Tests: the block's lines for enabled and missing methods; the endpoint for `ops_viewer`, never containing the encryption key, and 403 without an ops role; every configuration error above.
