# Sign-in methods: what you provide

This app can sign people in with email and password, authenticator apps, passkeys, Google, Apple and GitHub. Each method works once you give the app a few values from **your own** accounts: gorbital never owns them. This page walks through every value step by step: what it is, how to create it, where to paste it, and how to check it works.

Check what's on at any time:

```bash
go run ./cmd/api auth-providers     # the same "Sign-in methods" block the app prints when it starts in development
```

Operators can also read it from `GET /ops/auth/providers` (never values, only whether each method is configured).

Rules for every value:

- Paste it in `.env` in development, and in your secret store or deployment environment in production. Never commit it.
- An empty value turns its method off; the rest of the app keeps working.
- A half-filled method (for example an Android app without a fingerprint) stops the app at start with the variable to fix, so it can't silently stay off.
- Secrets can also be read from files: set `NAME_FILE=/run/secrets/name` instead of `NAME` where noted.

| Method | Variables | Secret? | You need | Status |
|---|---|---|---|---|
| [Email and password](#email-and-password) | Email provider (see [Email](#email-for-codes-and-alerts)) | Yes | A Resend account or an SMTP server | Always on |
| [Authenticator apps](#authenticator-apps) | `AUTH_ENCRYPTION_KEYS` | **Yes** | A terminal | Required in production |
| [Passkeys](#passkeys) | `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS` | No | Your domain | On in development (localhost) |
| [Passkeys in iOS apps](#passkeys-in-ios-apps) | `WEBAUTHN_APPLE_APP_IDS` | No | Apple Developer Program | Off until set |
| [Passkeys in Android apps](#passkeys-in-android-apps) | `WEBAUTHN_ANDROID_APPS` | No | Your app's signing keys | Off until set |
| [Google sign-in](#google-sign-in) | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_IOS_CLIENT_ID`, `GOOGLE_ANDROID_CLIENT_ID`, `APP_PUBLIC_URL` | Secret: yes | A Google account (free) | Off until set |
| [Apple sign-in](#apple-sign-in) | `APPLE_TEAM_ID`, `APPLE_SERVICES_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_FILE`, `APPLE_BUNDLE_IDS`, `APP_PUBLIC_URL` | Key: yes | Apple Developer Program (paid) | Off until set |
| [GitHub sign-in](#github-sign-in) | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `APP_PUBLIC_URL` | Secret: yes | A GitHub account (free) | Off until set |

Browser sign-in with Google, Apple or GitHub also needs [`AUTH_DEFAULT_RETURN_TO`](#after-signing-in) in production.

Console menus move from time to time. If a step's wording doesn't match what you see, look for the same item names nearby.

## Email and password

Nothing to configure for the method itself. Sign-up verification codes, password reset codes and security alerts are emails, so production needs an [email provider](#email-for-codes-and-alerts). In development every email lands in Mailpit at http://127.0.0.1:8025.

## Authenticator apps

Two-factor authentication with Google Authenticator, Microsoft Authenticator, 1Password, Authy and similar apps. Users scan a QR code from `POST /v1/auth/mfa/totp`. Ops roles (`platform_admin`, `ops_viewer`) require a second factor.

| Variable | What it is |
|---|---|
| `AUTH_ENCRYPTION_KEYS` | Encrypts each user's authenticator secret: comma-separated `id:base64key` entries, each a random 32-byte key. The first encrypts; all decrypt |

### Create a key

1. Generate it:

   ```bash
   # macOS, Linux, WSL
   echo "k1:$(openssl rand -base64 32)"
   ```

   ```powershell
   # Windows PowerShell 7
   "k1:" + [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
   ```

2. Paste the whole line, including `k1:`, as `AUTH_ENCRYPTION_KEYS=k1:…` in `.env`. In development `orb dev` does this for you when the value is empty.
3. In production, store it in your secret store (or mount a file and set `AUTH_ENCRYPTION_KEYS_FILE`). Use a different key from development.
4. Back it up like a database password. Losing every key turns off everyone's authenticator app until operators run `go run ./cmd/api reset-mfa <email>` for each user.

### Check it works

`go run ./cmd/api auth-providers` shows `✓ Authenticator apps (2FA)`. Without a key in production the app refuses to start.

### Replace a key

1. Generate `k2` the same way and put it first on every instance: `AUTH_ENCRYPTION_KEYS=k2:…,k1:…`.
2. Run `go run ./cmd/api rotate-auth-keys` to re-encrypt every secret with `k2`.
3. Remove `k1` from every instance.

## Passkeys

Sign in with Face ID, Touch ID, Windows Hello, an Android phone or a security key: without a password, or as the second factor after one. Browsers need no account with Apple or Google.

| Variable | What it is | Example |
|---|---|---|
| `WEBAUTHN_RP_ID` | Your site's domain as users see it (the "relying party ID"). Passkeys are tied to it: changing it later makes existing passkeys stop working | `example.com` |
| `WEBAUTHN_ORIGINS` | Comma-separated browser origins of the pages that use passkeys: your web frontend, and the API docs if you test there. Each must be on `WEBAUTHN_RP_ID` or a subdomain, and https in production | `https://example.com,https://app.example.com` |

### Development

Leave both empty. Passkeys work on `localhost` with `http://localhost:8080` and `http://localhost:3000`. Open the app at **`http://localhost:8080`**, not `127.0.0.1`: browsers don't allow passkeys on IP addresses.

### Production

1. **Pick the RP ID.** Use the registrable domain your users see, usually the bare domain (`example.com`), so passkeys work on `app.example.com`, `www.example.com` and your mobile apps alike. Choose carefully: it can't change without every user adding their passkeys again.
2. **List the origins.** Every page that calls `navigator.credentials`: for example `https://app.example.com`. Scheme and host only, no path or trailing slash.
3. Set both in your deployment environment:

   ```bash
   WEBAUTHN_RP_ID=example.com
   WEBAUTHN_ORIGINS=https://example.com,https://app.example.com
   ```

4. Add the same origins to `APP_CORS_ORIGINS` so browsers may call the API.

Empty in production turns passkeys off (503 `passkeys_unavailable`).

### Check it works

- `go run ./cmd/api auth-providers` shows `✓ Passkeys in browsers` with your RP ID and origins.
- Add a passkey from your frontend: `POST /v1/auth/passkeys/registration` returns `options` with `"rp": {"id": "example.com"}`.
- `SecurityError` in the browser means the page's origin isn't on the RP ID, or isn't https.

## Passkeys in iOS apps

Lets your iOS app use the same passkeys as your website. You need an [Apple Developer Program](https://developer.apple.com/programs/) membership.

| Variable | What it is | Example |
|---|---|---|
| `WEBAUTHN_APPLE_APP_IDS` | Comma-separated `TEAMID.bundle.id` of your iOS apps | `ABCDE12345.com.example.app` |

### Step by step

1. **Find your Team ID.** Sign in at [developer.apple.com/account](https://developer.apple.com/account) → **Membership details** → **Team ID** (10 characters, such as `ABCDE12345`).
2. **Find your Bundle ID.** In Xcode: select the project → your app target → **Signing & Capabilities** → **Bundle Identifier** (such as `com.example.app`). It's also listed at **Certificates, Identifiers & Profiles** → **Identifiers**.
3. **Set the variable**, joining them with a dot: `WEBAUTHN_APPLE_APP_IDS=ABCDE12345.com.example.app`. Separate several apps with commas.
4. **Add the entitlement.** In Xcode → target → **Signing & Capabilities** → **+ Capability** → **Associated Domains** → add `webcredentials:example.com` (your `WEBAUTHN_RP_ID`). Xcode enables Associated Domains on the App ID for you.
5. **Serve the file on your domain.** The API now serves `/.well-known/apple-app-site-association`. Apple fetches it from `https://<WEBAUTHN_RP_ID>/.well-known/apple-app-site-association` over https with no redirects. If your web frontend serves that domain rather than this API, have it proxy the path to the API.

### Check it works

```bash
curl -i https://example.com/.well-known/apple-app-site-association
# 200, Content-Type: application/json, and "webcredentials": {"apps": ["ABCDE12345.com.example.app"]}
```

Apple caches the file through its CDN: `https://app-site-association.cdn-apple.com/a/v1/example.com` shows what devices see, and can take a while to refresh. During development, append `?mode=developer` to the entitlement (`webcredentials:example.com?mode=developer`) and turn on **Settings → Developer → Associated Domains Development** on the device to skip the CDN.

## Passkeys in Android apps

Lets your Android app use the same passkeys as your website.

| Variable | What it is | Example |
|---|---|---|
| `WEBAUTHN_ANDROID_APPS` | Comma-separated `package.name=SHA256:FINGERPRINT` entries; join several fingerprints of one app with `+` | `com.example.app=SHA256:AB:CD:…:EF+SHA256:12:34:…:56` |

### Step by step

1. **Find the package name.** Your app's `applicationId` in `app/build.gradle(.kts)`, such as `com.example.app`.
2. **Collect every signing fingerprint (SHA-256)** that will run the app:
   - **Play App Signing** (apps on Google Play): [Play Console](https://play.google.com/console) → your app → **Test and release** → **App integrity** → **App signing** → **App signing key certificate** → **SHA-256 certificate fingerprint**. Users' installs are signed with this key.
   - **Upload key** (internal testing builds you install yourself): same page → **Upload key certificate** → SHA-256.
   - **Debug builds** on your machine:

     ```bash
     keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android -keypass android | grep SHA256
     # or, in the project: ./gradlew signingReport
     ```

3. **Set the variable**, joining the fingerprints with `+`:

   ```bash
   WEBAUTHN_ANDROID_APPS=com.example.app=SHA256:AB:CD:…:EF+SHA256:12:34:…:56
   ```

4. **Serve the file on your domain.** The API now serves `/.well-known/assetlinks.json` and accepts the app's `android:apk-key-hash:` origin. Google checks it at `https://<WEBAUTHN_RP_ID>/.well-known/assetlinks.json`; proxy the path from your frontend if needed.

### Check it works

```bash
curl -s https://example.com/.well-known/assetlinks.json
# lists com.example.app with delegate_permission/common.get_login_creds and your fingerprints

curl -s "https://digitalassetlinks.googleapis.com/v1/statements:list?source.web.site=https://example.com&relation=delegate_permission/common.get_login_creds"
# Google's own view of the file
```

A passkey prompt that never appears on Android usually means a missing fingerprint: debug, upload and Play App Signing keys all differ.

## Google sign-in

Sign in with a Google account, in browsers and in your iOS and Android apps ([ADR-0046](https://github.com/gorbital/gorbital/blob/main/docs/adr/0046-google-and-apple-sign-in.md)). Off until you set the variables below. You need a Google account; it's free.

| Variable | Secret? | What it is |
|---|---|---|
| `APP_PUBLIC_URL` | No | The API's public base URL, such as `https://api.example.com`; Google returns to it. Empty in development means `http://localhost:8080` |
| `GOOGLE_CLIENT_ID` | No | The **Web application** client ID, such as `1234-abc.apps.googleusercontent.com`. Android apps use it too |
| `GOOGLE_CLIENT_SECRET` | **Yes** (or `GOOGLE_CLIENT_SECRET_FILE`) | The web client's secret |
| `GOOGLE_IOS_CLIENT_ID` | No | The **iOS** client ID, if you have an iOS app |
| `GOOGLE_ANDROID_CLIENT_ID` | No | The **Android** client ID, if you have an Android app |

> [!WARNING]
> **Not the same as a Firebase service account key.** If this project also uses Firebase, **Project settings → Service accounts → Generate new private key** downloads a JSON file with `"type": "service_account"` and a `private_key` field. That key lets a server call Google APIs; it can't fill `GOOGLE_CLIENT_ID` or `GOOGLE_CLIENT_SECRET`, and pasting its contents anywhere won't work. Get the values below from **APIs & Services → Credentials** in the same Cloud project instead.

URLs to register (replace the domain with your API's):

| Environment | Authorized redirect URI |
|---|---|
| Development | `http://localhost:8080/v1/auth/google/callback` |
| Production | `https://api.example.com/v1/auth/google/callback` |

### 1. Create a project

1. Open [console.cloud.google.com](https://console.cloud.google.com) and sign in.
2. Project picker (top bar) → **New project** → name it after your app → **Create**. Select it.

Use one project per app; separate projects for development and production are optional.

### 2. Set up the consent screen (Google Auth Platform)

1. Menu → **APIs & Services** → **OAuth consent screen** (it opens **Google Auth Platform**) → **Get started**.
2. **App information**: app name users will see, and a support email → **Next**.
3. **Audience**: **External** (anyone with a Google account) → **Next**.
4. **Contact information**: your email → **Next** → agree → **Create**.
5. **Branding**: add your app's home page, privacy policy and terms links, and under **Authorized domains** your domain (`example.com`). A logo is optional; adding one needs Google's brand verification.
6. **Data access**: the app only needs `openid`, `email` and `profile`, which are non-sensitive; nothing to add.
7. **Audience**: while the status is **Testing**, only the **Test users** you list can sign in. When you're ready, click **Publish app** to allow everyone. With only basic scopes, no Google review is needed.

### 3. Create the web client

1. **Google Auth Platform** → **Clients** → **Create client**.
2. **Application type**: **Web application**. **Name**: such as `API (production)`.
3. **Authorized redirect URIs** → **Add URI**: the development and production URIs from the table above. They must match exactly: scheme, host, port and path, no trailing slash.
4. **Create**. Copy the **Client ID** and **Client secret** right away, or download the JSON: newer clients show the secret only at creation. Lost it? Open the client → **Add secret**, then delete the old one.
5. Paste them:

   ```bash
   GOOGLE_CLIENT_ID=1234-abc.apps.googleusercontent.com
   GOOGLE_CLIENT_SECRET=GOCSPX-…
   ```

### 4. iOS app (optional)

1. **Clients** → **Create client** → **iOS**.
2. **Bundle ID**: your app's, such as `com.example.app`. App Store ID and Team ID are optional.
3. **Create**, then copy the **Client ID** to `GOOGLE_IOS_CLIENT_ID`. The **iOS URL scheme** shown with it goes in the app's `Info.plist` for the Google Sign-In SDK.

### 5. Android app (optional)

1. **Clients** → **Create client** → **Android**.
2. **Package name**: your `applicationId`. **SHA-1 certificate fingerprint**: from the same places as [Android passkeys](#passkeys-in-android-apps), but the **SHA-1** line (Play Console → App integrity, or `./gradlew signingReport`). Create one Android client per signing key (debug, upload, Play App Signing).
3. **Create**, then copy one **Client ID** to `GOOGLE_ANDROID_CLIENT_ID`.
4. In the Android app, pass the **web** client ID (`GOOGLE_CLIENT_ID`) as `serverClientId` to Credential Manager's Sign in with Google: the ID tokens the API receives are issued for it.

### Common errors

| Error | Fix |
|---|---|
| `Error 400: redirect_uri_mismatch` | The URI in the error isn't in **Authorized redirect URIs**; add it exactly (http vs https, port, no trailing slash). Changes can take a few minutes |
| `Access blocked: … has not completed the Google verification process` | The app is in **Testing**: add the account under **Audience → Test users**, or publish the app |
| `invalid_client` | Wrong or deleted client secret; check `GOOGLE_CLIENT_SECRET` belongs to `GOOGLE_CLIENT_ID` |
| Android sign-in fails with `DEVELOPER_ERROR` or no accounts | The Android client's SHA-1 or package name doesn't match the build, or `serverClientId` isn't the web client ID |

## Apple sign-in

Sign in with an Apple Account, in browsers and in your iOS apps ([ADR-0046](https://github.com/gorbital/gorbital/blob/main/docs/adr/0046-google-and-apple-sign-in.md)). Off until you set the variables below. You need a paid [Apple Developer Program](https://developer.apple.com/programs/) membership. Apps on the App Store that offer Google sign-in must generally offer Sign in with Apple too.

| Variable | Secret? | What it is |
|---|---|---|
| `APP_PUBLIC_URL` | No | The API's public base URL, such as `https://api.example.com`; Apple returns to it |
| `APPLE_TEAM_ID` | No | Your 10-character Team ID |
| `APPLE_SERVICES_ID` | No | The Services ID for web sign-in, such as `com.example.web` |
| `APPLE_KEY_ID` | No | The 10-character ID of your Sign in with Apple key |
| `APPLE_PRIVATE_KEY_FILE` | **Yes** (or `APPLE_PRIVATE_KEY` with the file's contents) | Path to the `.p8` key file |
| `APPLE_BUNDLE_IDS` | No | Comma-separated bundle IDs of your iOS apps, for sign-in inside the apps |

URLs to register:

| Field | Value |
|---|---|
| Domains and Subdomains | `api.example.com` (your API's host, no scheme) |
| Return URLs | `https://api.example.com/v1/auth/apple/callback` |
| Server-to-Server Notification Endpoint | `https://api.example.com/v1/auth/apple/notifications` (on the App ID) |

Apple doesn't accept `localhost` or plain http. To try the web flow in development, expose the API through an https tunnel (for example `cloudflared tunnel --url http://localhost:8080` or `ngrok http 8080`) and register the tunnel's host and return URL too. Sign-in inside an iOS app works without one.

### 1. Find your Team ID

[developer.apple.com/account](https://developer.apple.com/account) → **Membership details** → **Team ID** → `APPLE_TEAM_ID`.

### 2. Create or update the App ID

Apple ties web sign-in to an app, even if you only have a website.

1. **Certificates, Identifiers & Profiles** → **Identifiers** → **+** → **App IDs** → **Continue** → **App** → **Continue**.
2. **Description**: your app's name. **Bundle ID**: **Explicit**, such as `com.example.app`.
3. Under **Capabilities**, tick **Sign in with Apple** (leave it as a primary App ID) → **Continue** → **Register**.

If the App ID exists, open it, tick **Sign in with Apple** and **Save**. Next to **Sign in with Apple**, click **Edit** (or **Configure**) and set **Server-to-Server Notification Endpoint** to `https://api.example.com/v1/auth/apple/notifications`: Apple then tells the app when someone stops using Sign in with Apple or deletes their Apple Account. Put iOS apps' bundle IDs in `APPLE_BUNDLE_IDS`, and in Xcode add the **Sign in with Apple** capability to the target.

### 3. Create the Services ID (web)

1. **Identifiers** → **+** → **Services IDs** → **Continue**.
2. **Description**: shown to users on Apple's sign-in page, such as your app's name. **Identifier**: you choose this yourself, like a username — any reverse-domain string that differs from the App ID and isn't already taken, such as `com.example.web` → **Continue** → **Register**.
3. Open the new Services ID → tick **Sign in with Apple** → **Configure**:
   - **Primary App ID**: the App ID from step 2.
   - **Domains and Subdomains**: `api.example.com` (and your tunnel's host for development).
   - **Return URLs**: `https://api.example.com/v1/auth/apple/callback` (and the tunnel's).
   - **Next** → **Done** → **Continue** → **Save**.
4. Copy the identifier to `APPLE_SERVICES_ID`.

### 4. Create the key

1. **Keys** → **+**.
2. **Key Name**: such as `Sign in with Apple`. Tick **Sign in with Apple** → **Configure** → choose the primary App ID → **Save** → **Continue** → **Register**.
3. **Download** the `.p8` file. **Apple lets you download it only once**; if you lose it, revoke the key and create another.
4. Copy the **Key ID** to `APPLE_KEY_ID`.
5. Store the file outside the repository and point to it:

   ```bash
   mkdir -p ~/.config/plateful && mv ~/Downloads/AuthKey_ABC123DEFG.p8 ~/.config/plateful/
   APPLE_PRIVATE_KEY_FILE=/Users/you/.config/plateful/AuthKey_ABC123DEFG.p8
   ```

   In production, mount it as a secret file, or put its full contents (including the `BEGIN`/`END` lines) in `APPLE_PRIVATE_KEY`.

### 5. Let Apple relay your emails

Users can hide their address; Apple then gives the app one like `abc123@privaterelay.appleid.com`, which only forwards mail from senders you register. Without this, verification codes and alerts to those users are dropped.

1. **Certificates, Identifiers & Profiles** → **Services** → **Sign in with Apple for Email Communication** → **Configure**.
2. **+** → add the domain you send from (the one in `mail.from_email`) and/or the exact sender address.
3. Make sure that domain has SPF (and ideally DKIM) records; your email provider shows them. Apple checks them before relaying.

### Common errors

| Error | Fix |
|---|---|
| `invalid_request` / `Invalid web redirect url` | The return URL isn't registered on the Services ID exactly as the API sends it, it's http/localhost, or step 3's edit was never saved — a row added in the URL editor isn't kept until you click **Done → Continue → Save** |
| `invalid_client` | `APPLE_SERVICES_ID`, `APPLE_TEAM_ID` or `APPLE_KEY_ID` doesn't match, or the key was revoked or isn't enabled for Sign in with Apple with this primary App ID |
| `{"code":"not_found", ... "no route matches GET /v1/auth/apple/callback"}` | You opened the callback URL directly in a browser | Expected — that route only answers Apple's **POST**. Start again from `/v1/auth/apple/start` |
| Users with hidden emails never get codes | Register your sending domain in step 5 and check SPF |
| The app says the key file can't be read | `APPLE_PRIVATE_KEY_FILE` must be an absolute path readable by the app; the file starts with `-----BEGIN PRIVATE KEY-----` |

## GitHub sign-in

Sign in with a GitHub account, in browsers only ([ADR-0059](https://github.com/gorbital/gorbital/blob/main/docs/adr/0059-github-sign-in.md)). Off until you set the variables below. You need a GitHub account; it's free. GitHub has no ID tokens for native apps: an iOS or Android app opens the browser flow instead.

| Variable | Secret? | What it is |
|---|---|---|
| `APP_PUBLIC_URL` | No | The API's public base URL, such as `https://api.example.com`; GitHub returns to it. Empty in development means `http://localhost:8080` |
| `GITHUB_CLIENT_ID` | No | The OAuth app's **Client ID**, such as `Ov23liAbCdEf12345678` |
| `GITHUB_CLIENT_SECRET` | **Yes** (or `GITHUB_CLIENT_SECRET_FILE`) | A client secret generated on the OAuth app's page |

The API asks GitHub only for `read:user` and `user:email`: the person's numeric ID, login, name and email addresses. It keeps the ID (logins can be renamed) and uses the **primary** address, only if GitHub has verified it. The access token is used for those two reads and never stored.

How accounts work with GitHub:

- A first GitHub sign-in creates an account with the primary verified address, **not verified** and without a password: GitHub doesn't host anyone's email, so it can't prove the person still owns it. The account signs in with GitHub as usual and isn't deleted as unverified; it verifies its address with a code (`POST /v1/auth/verify-email/resend`, then `POST /v1/auth/verify-email`) while signed in, before it can be given roles or accept invitations. Whoever else proves the address by email takes it over and removes the GitHub link.
- GitHub never links an existing account by itself, whatever the address: sign-in returns `#error=social_link_required`. The owner signs in and [links GitHub](#when-you-build-the-web-frontend) from their account.
- No verified primary address: sign-in returns `#error=social_email_unverified`. The person verifies it at GitHub → **Settings** → **Emails** and tries again.

URLs to register (replace the domain with your API's). An OAuth app has **one** callback URL, so create one app per environment:

| Environment | Homepage URL | Authorization callback URL |
|---|---|---|
| Development | `http://localhost:8080` | `http://localhost:8080/v1/auth/github/callback` |
| Production | `https://example.com` | `https://api.example.com/v1/auth/github/callback` |

### 1. Create the OAuth app

1. Sign in at [github.com](https://github.com). For a company app, create it under your organisation so it doesn't depend on one person: the organisation's **Settings** → **Developer settings** → **OAuth Apps** → **New OAuth App**. For a personal one: your picture (top right) → **Settings** → **Developer settings** → **OAuth Apps** → **New OAuth App**.
2. **Application name**: what people see when they approve, such as `Acme`. **Homepage URL**: your site. **Application description**: optional.
3. **Authorization callback URL**: the one for this environment from the table above.
4. Leave **Enable Device Flow** unticked. Click **Register application**.

Use an **OAuth app**, not a GitHub App: the API asks for OAuth scopes.

### 2. Copy the client ID and create a secret

1. On the app's page, copy the **Client ID** to `GITHUB_CLIENT_ID`.
2. Click **Generate a new client secret** (GitHub may ask you to confirm with your password or 2FA) and copy it straight away: GitHub shows it only once. Lost it? Generate another, switch the app to it, then delete the old one.
3. Paste them:

   ```bash
   GITHUB_CLIENT_ID=Ov23liAbCdEf12345678
   GITHUB_CLIENT_SECRET=0123456789abcdef0123456789abcdef01234567
   ```

4. Optional: upload a logo under **Application logo**; people see it on GitHub's approval page.

### Check it works

`go run ./cmd/api auth-providers` shows `✓ GitHub sign-in` with the callback URL. Open `http://localhost:8080/v1/auth/github/start` in a browser, approve, and you arrive at the API docs signed in; `http://localhost:8080/v1/auth/me` shows the account.

### Common errors

| Error | Fix |
|---|---|
| GitHub shows `The redirect_uri is not associated with this application` | The app's **Authorization callback URL** isn't `<APP_PUBLIC_URL>/v1/auth/github/callback` for this environment (scheme, host and port must match); edit it and **Update application**, or use this environment's app |
| Back on your site with `#error=invalid_social_token`, and the log says `incorrect_client_credentials` | `GITHUB_CLIENT_SECRET` isn't a current secret of `GITHUB_CLIENT_ID`'s app |
| `#error=invalid_social_token`, and the log says `bad_verification_code` | The code was used, expired, or started in another sign-in; start again |
| `#error=social_email_unverified` | The GitHub account has no verified primary email; verify it at GitHub → **Settings** → **Emails** |
| `#error=social_link_required` | The address already has an account: sign in to it and link GitHub |
| `#error=access_denied` | The person clicked **Cancel** at GitHub |

## After signing in

A browser sign-in with Google, Apple or GitHub returns to the `return_to` your frontend passes to `/v1/auth/{provider}/start`: an address on `APP_PUBLIC_URL` or an `APP_CORS_ORIGINS` origin. Without one, and when a sign-in fails before the API knows its `return_to` (an expired or unknown state), the browser goes to `AUTH_DEFAULT_RETURN_TO`.

| Variable | Secret? | What it is |
|---|---|---|
| `AUTH_DEFAULT_RETURN_TO` | No | A page of your frontend that reads the fragment (`#error=…`, `#mfa_challenge_token=…`), such as `https://app.example.com/signed-in`. Same origin rules as `return_to`; https in production; no `#` |

- **Development:** empty means the API docs, `http://localhost:8080/docs` (set it when `APP_DOCS_ENABLED=false`).
- **Production:** required with Google, Apple on the web, or GitHub, and the app refuses to start without it: the API's `/docs` is off in production, so a default there would end sign-ins on a 404.

## Email for codes and alerts

Not a sign-in method, but every method relies on it in production: verification and reset codes, "passkey added" and recovery-code alerts. Configure it with `orb add mail`, which asks for the provider and writes `.env`.

**Resend**

1. Create an account at [resend.com](https://resend.com).
2. **Domains** → **Add domain** → your sending domain → add the DNS records it shows (SPF, DKIM, and the MX record for bounces) at your DNS host → **Verify**.
3. **API Keys** → **Create API key** → permission **Sending access**, limited to that domain → copy it (shown once) → `RESEND_API_KEY=re_…`.
4. Set the sender to an address on the domain: `PUT /ops/settings/mail.from_email`.

**SMTP** (Amazon SES, Postmark, Mailgun, your own server): set `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME` and `SMTP_PASSWORD` from your provider's SMTP settings page.

## Other secrets

| Variable | What it is | Where it comes from |
|---|---|---|
| `DATABASE_URL` (or `DATABASE_URL_FILE`) | PostgreSQL connection string | Development: `compose.yaml`, already in `.env`. Production: your database provider's connection string with `sslmode=require` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Where traces and metrics go (optional) | Your observability backend |

## Before going to production

- [ ] `APP_ENV=production` and a production `AUTH_ENCRYPTION_KEYS`, backed up.
- [ ] Email provider configured; sender domain verified; registered with Apple's relay if you use Apple sign-in.
- [ ] `WEBAUTHN_RP_ID` and https `WEBAUTHN_ORIGINS` set; the same origins in `APP_CORS_ORIGINS`.
- [ ] `/.well-known/apple-app-site-association` and `assetlinks.json` reachable on the RP ID's domain, if you have mobile apps.
- [ ] `APP_PUBLIC_URL` set to the API's https URL, and `AUTH_DEFAULT_RETURN_TO` to a page of your frontend, if you use Google, Apple or GitHub.
- [ ] Google: consent screen published; production redirect URI registered; secret stored in the secret store.
- [ ] Apple: production domain and return URL on the Services ID; notification endpoint on the App ID; `.p8` stored as a secret file; sending domain registered for email relay.
- [ ] GitHub: a production OAuth app with the production callback URL; its secret stored in the secret store.
- [ ] `go run ./cmd/api auth-providers` in the production environment shows every method you expect as `✓`.

## When you build the web frontend

- Sign in with a passkey: `POST /v1/auth/passkeys/login/options`, pass `options` to `navigator.credentials.get()` (or `PublicKeyCredential.parseRequestOptionsFromJSON`), send the result's `toJSON()` to `POST /v1/auth/passkeys/login`.
- Add a passkey: `POST /v1/auth/passkeys/registration`, `navigator.credentials.create()`, `POST /v1/auth/passkeys`.
- Second factor: after a 202 from `POST /v1/auth/login`, offer the `methods` it lists; for `passkey`, call `POST /v1/auth/login/mfa/passkey` first.
- Confirm sensitive changes (delete the account, turn off the authenticator app, replace recovery codes) with a passkey: `POST /v1/auth/passkeys/verification`, `navigator.credentials.get()`, then send the result as `passkey`. Adding or removing a passkey asks for the password once the sign-in is 10 minutes old.
- Authenticator apps: show `qr_code` from `POST /v1/auth/mfa/totp` as an image, then confirm a code.
- Google, Apple and GitHub: link or redirect the browser (not `fetch`) to `/v1/auth/google/start?return_to=https://app.example.com/after-login` (or `/apple/start`, `/github/start`). The API sends the browser back to `return_to` signed in (session cookie set), with `#mfa_challenge_token=…&methods=…` to finish with `POST /v1/auth/login/mfa`, or with `#error=<code>`. Read the fragment, then clear it from the address bar.
- Accounts created with Google, Apple or GitHub have no password (`user.has_password` is false): hide "change password", and let them set one with "forgot password". Linked accounts: `GET /v1/auth/identities`, `DELETE /v1/auth/identities/{id}`.
- `#error=social_link_required`: the address has an account, and Google or Apple doesn't manage the address (only Gmail, the person's Google Workspace domain, iCloud and Apple relay addresses link by themselves). Ask the person to sign in with their password, then link: get an ID token with Google Identity Services or Sign in with Apple JS using a nonce from `POST /v1/auth/{provider}/nonce`, and send it with the password to `POST /v1/auth/identities`.
- Link GitHub (always required for an existing account, and GitHub has no ID token): from the signed-in page, `fetch("<API>/v1/auth/github/link", {method: "POST", credentials: "include", headers: {"Content-Type": "application/json"}, body: JSON.stringify({password, return_to: "https://app.example.com/settings"})})`, then `window.location = response.url`. The request sets a short-lived cookie in this browser; GitHub returns to `return_to` with GitHub linked, or with `#error=identity_in_use`, `#error=unauthenticated` (the session ended) or `#error=invalid_state`. It works when the frontend and API share a site (`app.example.com` and `api.example.com`); browsers that block third-party cookies refuse the cookie for a frontend on another site.
- Accounts created with GitHub start unverified: offer "verify your email" (`POST /v1/auth/verify-email/resend`, then `POST /v1/auth/verify-email` while signed in).
- `invalid_credentials` right after verifying an address: it was registered more than once with different passwords before verification, so it has none; offer "forgot password".
- Add the frontend's origin to `WEBAUTHN_ORIGINS` and `APP_CORS_ORIGINS` (the second also allows it as a `return_to`).

## When you build the iOS app

- Add the **Associated Domains** capability with `webcredentials:<WEBAUTHN_RP_ID>`, and set `WEBAUTHN_APPLE_APP_IDS`.
- Use `ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: "<WEBAUTHN_RP_ID>")` with the API's options, and send the results to the same endpoints as the web frontend with `"transport": "bearer"`.
- Sign in with Apple: add the **Sign in with Apple** capability. Get a nonce from `POST /v1/auth/apple/nonce`, set `request.nonce` to its SHA-256 in hex, and send `identityToken`, `authorizationCode`, the raw nonce and `fullName` (first time only) to `POST /v1/auth/apple/token` with `"transport": "bearer"`.
- Google: add the Google Sign-In URL scheme, get a nonce from `POST /v1/auth/google/nonce`, pass it to `GIDSignIn.signIn(withPresenting:hint:additionalScopes:nonce:)`, and send `idToken` and the nonce to `POST /v1/auth/google/token`.
- Both return a session, or 202 with a second-factor challenge like `POST /v1/auth/login`, or 403 `social_link_required` for an existing account's address the provider doesn't manage: the person signs in with their password, and the app sends a new ID token and nonce with the password to `POST /v1/auth/identities`.
- Store the session token in the Keychain.

## When you build the Android app

- Set `WEBAUTHN_ANDROID_APPS` with every signing key's SHA-256 fingerprint (debug, upload, Play App Signing).
- Use Credential Manager (`CreatePublicKeyCredentialRequest`, `GetPublicKeyCredentialOption`) with the API's options as JSON, and send the results with `"transport": "bearer"`.
- For Google sign-in, get a nonce from `POST /v1/auth/google/nonce`, use Credential Manager's `GetGoogleIdOption` with `setServerClientId(<GOOGLE_CLIENT_ID>)` and `setNonce(nonce)`, and send the `idToken` and nonce to `POST /v1/auth/google/token` with `"transport": "bearer"`.
- Store the session token with the Android Keystore.
