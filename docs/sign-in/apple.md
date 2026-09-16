# Apple sign-in

Let people sign in with their Apple Account, on your website and in your iOS apps. This page starts from nothing. You need a paid [Apple Developer Program](https://developer.apple.com/programs/) membership (a yearly fee), and about 30 minutes.

If your iOS app offers Google sign-in, the App Store generally requires Sign in with Apple as well.

## What you'll end up with

| Variable | Required? | Secret? | Looks like | How you get it |
|---|---|---|---|---|
| `APPLE_TEAM_ID` | Yes | No | `ABCDE12345` | Copied from your Apple Developer account |
| `APPLE_KEY_ID` | Yes | No | `XYZ987WVU6` | Copied from Apple, when you create the key |
| `APPLE_PRIVATE_KEY_FILE` | Yes (or `APPLE_PRIVATE_KEY`) | **Yes**, the file it points to | `/run/secrets/AuthKey_XYZ987WVU6.p8` | A `.p8` file you download from Apple, **once** |
| `APPLE_SERVICES_ID` | For sign-in on websites | No | `com.example.web` | You choose it, and register it at Apple |
| `APPLE_BUNDLE_IDS` | For sign-in inside iOS apps | No | `com.example.app` | Your iOS app's bundle ID |
| `APP_PUBLIC_URL` | For websites | No | `https://api.example.com` | Your API's own https address |

You need at least one of `APPLE_SERVICES_ID` and `APPLE_BUNDLE_IDS`, and all of the first three.

> [!NOTE]
> **There is no Apple client secret to create.** Apple's own guides tell you to make a "client secret" by signing a token with your key and to renew it every few months. Your API does this for you: each time it talks to Apple, it creates a fresh one from `APPLE_TEAM_ID`, `APPLE_KEY_ID` and the `.p8` key, valid for 5 minutes. You never generate or store one, and there's no variable for it.

## Words on this page

| Word | What it is |
|---|---|
| **Team ID** | Your developer account's 10-character ID |
| **App ID** | Your app's registration at Apple, named by its bundle ID such as `com.example.app`. Apple ties web sign-in to an App ID too, even if you only have a website |
| **Services ID** | The registration for signing in on **websites**, such as `com.example.web`. It says which domains and return URLs Apple may send people back to |
| **Key** (`.p8` file) | A private key that proves requests come from your team. Apple lets you download it once |
| **Key ID** | The key's 10-character name |
| **Return URL** | The address on your API where Apple sends people back: Apple's name for a redirect URI |

## How it works

1. Your website sends the browser to your API: `/v1/auth/apple/start`.
2. Your API sends the browser on to Apple, with your Services ID.
3. The person signs in at Apple. The first time, they choose whether to share their email address or hide it behind a relay address.
4. Apple sends the browser back with a form **POST** to your API's return URL, `/v1/auth/apple/callback`.
5. Your API checks Apple's answer with a client secret it signs with your key, signs the person in and sends them back to your website.

## Your URLs

| Field at Apple | Value (replace `api.example.com` with your API's host) |
|---|---|
| Domains and Subdomains | `api.example.com` (host only: no `https://`, no path) |
| Return URLs | `https://api.example.com/v1/auth/apple/callback` |
| Server-to-Server Notification Endpoint | `https://api.example.com/v1/auth/apple/notifications` |

Apple accepts only **https** addresses, and never `localhost` or IP addresses. For staging, register its host and return URL as well: a Services ID can hold several.

## Step 1: Find your Team ID

<div class="steps">

1. **Open your developer account**

   Sign in at [developer.apple.com/account](https://developer.apple.com/account) with the Apple Account that belongs to your developer membership.

2. **Copy the Team ID**

   Scroll to **Membership details**. Copy **Team ID**, 10 letters and digits:

   ```bash
   APPLE_TEAM_ID=ABCDE12345
   ```

</div>

## Step 2: Create or update the App ID

<div class="steps">

1. **Open Identifiers**

   In your account, open **Certificates, IDs & Profiles** → **Identifiers**.

2. **If your app already has an App ID**

   Click it, tick **Sign in with Apple** under **Capabilities**, and click **Save**. Skip to step 5 below.

3. **Otherwise, register one**

   Click **+** → **App IDs** → **Continue** → **App** → **Continue**. **Description**: your app's name. **Bundle ID**: **Explicit**, such as `com.example.app`. If you have an iOS app, use its exact bundle ID from Xcode.

4. **Turn on Sign in with Apple**

   Under **Capabilities**, tick **Sign in with Apple**, leave it as a primary App ID, then **Continue** → **Register**.

5. **Add the notification endpoint**

   Open the App ID again. Next to **Sign in with Apple**, click **Edit** (or **Configure**). In **Server-to-Server Notification Endpoint**, enter `https://api.example.com/v1/auth/apple/notifications` and save. Apple then tells your API when someone stops using Sign in with Apple with your app or deletes their Apple Account, and your API acts on it.

6. **If you have an iOS app**

   Put its bundle ID in `APPLE_BUNDLE_IDS`, and in Xcode add the **Sign in with Apple** capability to your target (**Signing & Capabilities** → **+ Capability**).

</div>

## Step 3: Create the Services ID (websites)

Skip this step if you only need sign-in inside iOS apps.

<div class="steps">

1. **Register it**

   **Identifiers** → **+** → **Services IDs** → **Continue**. **Description**: shown to people on Apple's sign-in page, so use your app's name. **Identifier**: a reverse-domain name that differs from the App ID, such as `com.example.web`. Click **Continue** → **Register**.

2. **Configure Sign in with Apple**

   Click the new Services ID, tick **Sign in with Apple**, and click **Configure**.

3. **Fill in the website details**

   - **Primary App ID**: the App ID from step 2.
   - **Domains and Subdomains**: `api.example.com`.
   - **Return URLs**: `https://api.example.com/v1/auth/apple/callback`.

   Click **Next** → **Done** → **Continue** → **Save**.

4. **Copy the identifier**

   ```bash
   APPLE_SERVICES_ID=com.example.web
   ```

</div>

## Step 4: Create the key

<div class="steps">

1. **Start a key**

   **Certificates, IDs & Profiles** → **Keys** → **+**.

2. **Name and enable it**

   **Key Name**: such as `acme Sign in with Apple`. Tick **Sign in with Apple** → **Configure** → choose your primary App ID from step 2 → **Save** → **Continue** → **Register**.

3. **Download the file**

   Click **Download**. You get a file named like `AuthKey_XYZ987WVU6.p8`. **Apple lets you download it only once.** If you lose it, revoke the key on the same page and create a new one.

4. **Copy the Key ID**

   It's on the page, and in the file name after `AuthKey_`:

   ```bash
   APPLE_KEY_ID=XYZ987WVU6
   ```

</div>

The file is plain text. It should look like this, with different characters in the middle:

```text
-----BEGIN PRIVATE KEY-----
MIGTAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBHkwdwIBAQQg…
-----END PRIVATE KEY-----
```

## Step 5: Store the key

The `.p8` file is the most sensitive value on this page: with it, anyone can act as your app at Apple. Keep it out of your repository.

**On your computer**, move it to a folder outside your app and point to it with a full path:

```bash
mkdir -p ~/.config/acme-api
mv ~/Downloads/AuthKey_XYZ987WVU6.p8 ~/.config/acme-api/
chmod 600 ~/.config/acme-api/AuthKey_XYZ987WVU6.p8
```

```bash
# in .env: a full path, not ~ and not a path inside the repository
APPLE_PRIVATE_KEY_FILE=/Users/you/.config/acme-api/AuthKey_XYZ987WVU6.p8
```

**In production**, use one of these:

- **A secret file** (preferred): upload the `.p8` as a secret that your platform mounts as a file (Kubernetes secrets, Docker secrets, Fly.io or Render secret files), and set `APPLE_PRIVATE_KEY_FILE` to where it's mounted, such as `/run/secrets/apple_private_key`.
- **A secret value**: if your platform only has environment variables, set `APPLE_PRIVATE_KEY` to the whole contents of the file, including the `BEGIN` and `END` lines and the line breaks.

Set one or the other, never both: the app refuses to start when both are set.

## Step 6: Let Apple forward emails

People can hide their email address. Apple then gives your app an address like `abc123@privaterelay.appleid.com`, and forwards email sent to it, but **only from senders you've registered**. Without this step, those people never receive their sign-up codes.

<div class="steps">

1. **Open the email settings**

   **Certificates, IDs & Profiles** → **Services** → **Sign in with Apple for Email Communication** → **Configure**.

2. **Add your sender**

   Click **+** and add the domain you send email from (the domain of `mail.from_email`), or the exact sender address.

3. **Check your domain's records**

   Apple checks that the domain has an SPF record (and DKIM, ideally). Your email provider showed you these when you verified the domain ([Email sending](email.md)).

</div>

## Step 7: Put the values in your app

A website and an iOS app, in production:

```bash
APP_PUBLIC_URL=https://api.example.com
APPLE_TEAM_ID=ABCDE12345
APPLE_SERVICES_ID=com.example.web
APPLE_KEY_ID=XYZ987WVU6
APPLE_PRIVATE_KEY_FILE=/run/secrets/apple_private_key
APPLE_BUNDLE_IDS=com.example.app
```

Only an iOS app: leave `APPLE_SERVICES_ID` empty. `APP_PUBLIC_URL` is then only needed for other providers.

Restart the app after changing them.

## Step 8: Try it

### Inside your iOS app

This works on your computer without anything extra, because it doesn't use return URLs.

1. Get a one-time value from `POST /v1/auth/apple/nonce`.
2. With `ASAuthorizationAppleIDProvider`, set `request.nonce` to the SHA-256 of that value, as lowercase hex.
3. Send `identityToken`, `authorizationCode`, the original value, and the person's name (Apple only gives it the first time) to `POST /v1/auth/apple/token` with `"transport": "bearer"`.
4. Your API answers with a session token, or with a second-factor challenge if the account has one. Store the token in the Keychain.

### On a website, from your computer

Apple needs a public https address, so give your computer one with a tunnel. For example, with Cloudflare's free tunnel tool ([install `cloudflared`](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)):

```bash
cloudflared tunnel --url http://localhost:8080
```

It prints an address such as `https://random-words.trycloudflare.com`. Then:

1. Add that host to the Services ID's **Domains and Subdomains**, and `https://random-words.trycloudflare.com/v1/auth/apple/callback` to its **Return URLs**.
2. In `.env`, set `APP_PUBLIC_URL=https://random-words.trycloudflare.com` and restart.
3. In your browser, open `https://random-words.trycloudflare.com/v1/auth/apple/start`, using the tunnel address throughout rather than localhost.
4. Sign in at Apple. You come back to `/docs` on the tunnel address, signed in; `/v1/auth/me` shows the account.

A quick tunnel's address changes each time you start it, so remove old ones from the Services ID when you're done. `ngrok http 8080` works the same way.

### People who already have an account

Signing in with Apple links to an existing account with the same email address only when Apple manages that address: an iCloud address (`icloud.com`, `me.com`, `mac.com`) or a private relay address. For any other address, Apple only checked it once, when it was added to the Apple Account, and the person may have lost the mailbox since. Sign-in then answers `social_link_required` (in the web flow, `#error=social_link_required`). The person signs in with their password and links Apple from their account: your app gets an identity token with a nonce from `POST /v1/auth/apple/nonce` (Sign in with Apple JS on a website, `ASAuthorizationAppleIDProvider` in iOS) and sends it, with the password, to `POST /v1/auth/identities`.

Your API accepts each server-to-server notification once, and only within an hour of Apple signing it, so a copy of an old notification can't unlink Apple again later.

## Check it works

- `go run ./cmd/api auth-providers` shows `✓ Apple sign-in` (websites) and `✓ Apple sign-in in iOS apps`.
- `GET /ops/auth/providers` shows the same to administrators.
- Signing in through `/v1/auth/apple/start` ends back on your site, signed in.

## If something goes wrong

| What you see | What it means | Fix |
|---|---|---|
| The app won't start: `sign-in with Apple also needs APPLE_KEY_ID, …` | Some Apple values are set and others are missing | Set the variables it lists, or empty every `APPLE_` variable to turn Apple off |
| The app won't start: `APPLE_PRIVATE_KEY_FILE: … isn't a PEM private key` | The file isn't the `.p8`, or the value lost its `BEGIN`/`END` lines | Point to the downloaded `.p8`; with `APPLE_PRIVATE_KEY`, paste the whole file including line breaks |
| The app won't start: `the Apple key must be a P-256 key` | A different kind of key, such as an APNs certificate | Create a key with **Sign in with Apple** ticked |
| The app won't start: `read APPLE_PRIVATE_KEY_FILE` | The path is wrong or not readable by the app | Use a full path, and check the file's permissions |
| The app won't start: `both variable and _FILE variant are set: APPLE_PRIVATE_KEY` | Both ways of giving the key are set | Keep one |
| Apple shows `invalid_request` or `Invalid redirect_uri` | The return URL isn't registered exactly, or isn't https | Add `<APP_PUBLIC_URL>/v1/auth/apple/callback` to the Services ID exactly |
| Apple's sign-in fails with `invalid_client` | The Services ID, Team ID or Key ID doesn't match, or the key was revoked or belongs to another App ID | Check all three values, and that the key is enabled for Sign in with Apple with the Services ID's primary App ID |
| People with hidden emails never get codes | Apple doesn't relay from your sender | Do [step 6](#step-6-let-apple-forward-emails) |
| iOS: sign-in fails at your API | The app's bundle ID isn't in `APPLE_BUNDLE_IDS`, or the nonce wasn't hashed | Add the bundle ID; send the original value, and give Apple its SHA-256 |
| Sign-in answers `social_link_required` | The address has an account and isn't an iCloud or relay address | Sign in with the password and link Apple with `POST /v1/auth/identities` ([above](#people-who-already-have-an-account)) |
