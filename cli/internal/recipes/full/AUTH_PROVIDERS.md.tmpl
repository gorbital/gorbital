# Sign-in methods: what you provide

This app can sign people in with email and password, authenticator apps, passkeys, and (soon) Google and Apple. Each method works once you give the app a few values from **your own** accounts. This page lists every value: what it is, where to find it, and where to paste it.

Check what's on at any time:

```bash
go run ./cmd/api auth-providers     # the same "Sign-in methods" block the app prints when it starts in development
```

Operators can also read it from `GET /ops/auth/providers` (never values, only whether each method is configured).

Rules for every value:

- Paste it in `.env` in development, and in your secret store or deployment environment in production. Never commit it.
- An empty value turns its method off; the rest of the app keeps working.
- A half-filled method (for example an Android app without a fingerprint) stops the app at start with the variable to fix, so it can't silently stay off.

| Method | Variables | Secret? | Status |
|---|---|---|---|
| [Email and password](#email-and-password) | none | | Always on |
| [Authenticator apps](#authenticator-apps) | `AUTH_ENCRYPTION_KEYS` | **Yes** | Required in production |
| [Passkeys](#passkeys) | `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS` | No | On in development (localhost) |
| [Passkeys in iOS apps](#passkeys-in-ios-apps) | `WEBAUTHN_APPLE_APP_IDS` | No | Off until set |
| [Passkeys in Android apps](#passkeys-in-android-apps) | `WEBAUTHN_ANDROID_APPS` | No | Off until set |
| [Google sign-in](#google-sign-in) | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, … | Secret: yes | Coming in a later version |
| [Apple sign-in](#apple-sign-in) | `APPLE_TEAM_ID`, `APPLE_SERVICES_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_FILE`, … | Key: yes | Coming in a later version |

## Email and password

Nothing to configure. Emails (verification and reset codes) go to Mailpit in development and through your email provider in production; see `aps add mail`.

## Authenticator apps

Two-factor authentication with Google Authenticator, Microsoft Authenticator, 1Password, Authy and similar apps. Ops roles (`platform_admin`, `ops_viewer`) require a second factor.

| Variable | What it is | How to get it |
|---|---|---|
| `AUTH_ENCRYPTION_KEYS` | Encrypts each user's authenticator secret: comma-separated `id:base64key` entries, the first encrypting | `echo "k1:$(openssl rand -base64 32)"` |

- Development: `aps dev` writes a random key to `.env` when it's empty.
- Production: required; the app doesn't start without it. Store it like a database password, and back it up: losing every key turns off everyone's authenticator app until operators reset them.
- Replacing a key: put the new key first (`k2:…,k1:…`) on every instance, run `go run ./cmd/api rotate-auth-keys`, then remove `k1`.

## Passkeys

Sign in with Face ID, Touch ID, Windows Hello, an Android phone or a security key: without a password, or as the second factor after one. No account with Apple or Google is needed for browsers.

| Variable | What it is | Example |
|---|---|---|
| `WEBAUTHN_RP_ID` | Your site's domain as users see it (the "relying party ID"). Passkeys are tied to it: changing it later makes existing passkeys stop working | `example.com` |
| `WEBAUTHN_ORIGINS` | Comma-separated browser origins of the pages that use passkeys: your web frontend, and the API docs if you test there. Each must be on `WEBAUTHN_RP_ID` or a subdomain, and https in production | `https://example.com,https://app.example.com` |

- Development: leave both empty to use `localhost` with `http://localhost:8080` and `http://localhost:3000`. Open the app at **`http://localhost:8080`**, not `127.0.0.1`: browsers don't allow passkeys on IP addresses.
- Production: empty turns passkeys off (503 `passkeys_unavailable`).

## Passkeys in iOS apps

Lets your iOS app use the same passkeys as your website.

| Variable | What it is | Where to find it |
|---|---|---|
| `WEBAUTHN_APPLE_APP_IDS` | Comma-separated `TEAMID.bundle.id` of your iOS apps | **Team ID**: [Apple Developer](https://developer.apple.com/account) → Membership details. **Bundle ID**: Xcode → your target → Signing & Capabilities, or Certificates, Identifiers & Profiles → Identifiers |

Example: `WEBAUTHN_APPLE_APP_IDS=ABCDE12345.com.example.app`

With it set, the API serves `/.well-known/apple-app-site-association`. Apple fetches that file from `https://<WEBAUTHN_RP_ID>/.well-known/apple-app-site-association`: if your web frontend serves that domain rather than this API, have it proxy the path to the API.

## Passkeys in Android apps

Lets your Android app use the same passkeys as your website.

| Variable | What it is | Where to find it |
|---|---|---|
| `WEBAUTHN_ANDROID_APPS` | Comma-separated `package.name=SHA256:FINGERPRINT` entries; join several fingerprints of one app with `+` | **Package name**: your app's `applicationId` in `build.gradle`. **Fingerprint**: Play Console → your app → Test and release → App integrity → App signing key certificate → SHA-256. For debug builds: `keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android` |

Example: `WEBAUTHN_ANDROID_APPS=com.example.app=SHA256:AB:CD:…:EF+SHA256:12:34:…:56` (release and debug keys)

With it set, the API serves `/.well-known/assetlinks.json` and accepts the app's origin. Google checks it at `https://<WEBAUTHN_RP_ID>/.well-known/assetlinks.json`; proxy the path from your frontend if needed.

## Google sign-in

Coming in a later version. You'll need, from [Google Cloud Console](https://console.cloud.google.com) → APIs & Services:

| Variable | Secret? | Where to find it |
|---|---|---|
| `GOOGLE_CLIENT_ID` | No | Credentials → Create credentials → OAuth client ID → **Web application** (configure the OAuth consent screen first) |
| `GOOGLE_CLIENT_SECRET` | **Yes** (`GOOGLE_CLIENT_SECRET_FILE` also works) | Shown with the web client ID |
| `GOOGLE_IOS_CLIENT_ID` | No | OAuth client ID → **iOS**, with your bundle ID |
| `GOOGLE_ANDROID_CLIENT_ID` | No | OAuth client ID → **Android**, with your package name and SHA-1 fingerprint |

Register the redirect URI `https://<your API>/v1/auth/google/callback` (development: `http://localhost:8080/v1/auth/google/callback`).

## Apple sign-in

Coming in a later version. You'll need, from [Apple Developer](https://developer.apple.com/account):

| Variable | Secret? | Where to find it |
|---|---|---|
| `APPLE_TEAM_ID` | No | Membership details → Team ID |
| `APPLE_SERVICES_ID` | No | Certificates, Identifiers & Profiles → Identifiers → **Services IDs** → new, with Sign in with Apple enabled |
| `APPLE_KEY_ID` | No | Keys → new key with Sign in with Apple → Key ID |
| `APPLE_PRIVATE_KEY_FILE` | **Yes** (or `APPLE_PRIVATE_KEY` with the contents) | The `.p8` file, downloadable only once when you create the key; keep it outside the repository |
| `APPLE_BUNDLE_IDS` | No | Your iOS apps' bundle IDs |

Register your domain and the return URL `https://<your API>/v1/auth/apple/callback` in the Services ID. Apple doesn't accept `localhost` for the web flow: test through a tunnel, or with the iOS app.

## When you build the web frontend

- Sign in with a passkey: `POST /v1/auth/passkeys/login/options`, pass `options` to `navigator.credentials.get()` (or `PublicKeyCredential.parseRequestOptionsFromJSON`), send the result's `toJSON()` to `POST /v1/auth/passkeys/login`.
- Add a passkey: `POST /v1/auth/passkeys/registration`, `navigator.credentials.create()`, `POST /v1/auth/passkeys`.
- Second factor: after a 202 from `POST /v1/auth/login`, offer the `methods` it lists; for `passkey`, call `POST /v1/auth/login/mfa/passkey` first.
- Authenticator apps: show `qr_code` from `POST /v1/auth/mfa/totp` as an image, then confirm a code.
- Add the frontend's origin to `WEBAUTHN_ORIGINS` and `APP_CORS_ORIGINS`.

## When you build the iOS app

- Add the **Associated Domains** capability with `webcredentials:<WEBAUTHN_RP_ID>`, and set `WEBAUTHN_APPLE_APP_IDS`.
- Use `ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: "<WEBAUTHN_RP_ID>")` with the API's options, and send the results to the same endpoints as the web frontend with `"transport": "bearer"`.
- Store the session token in the Keychain.

## When you build the Android app

- Set `WEBAUTHN_ANDROID_APPS` with every signing key's SHA-256 fingerprint (debug, release, Play App Signing).
- Use Credential Manager (`CreatePublicKeyCredentialRequest`, `GetPublicKeyCredentialOption`) with the API's options as JSON, and send the results with `"transport": "bearer"`.
- Store the session token with the Android Keystore.
