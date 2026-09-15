# Google sign-in

Let people sign in with their Google account, on your website and in your iOS and Android apps. This page starts from nothing: no Google Cloud account, no project. It takes about 15 minutes, and Google charges nothing for it.

## What you'll end up with

| Variable | Required? | Secret? | Looks like | How you get it |
|---|---|---|---|---|
| `GOOGLE_CLIENT_ID` | Yes, to turn Google on | No | `1234567890-abc123.apps.googleusercontent.com` | Copied from Google Cloud Console (web client) |
| `GOOGLE_CLIENT_SECRET` | Yes, with the client ID | **Yes** | `GOCSPX-…` | Copied from Google Cloud Console, when you create the web client |
| `GOOGLE_IOS_CLIENT_ID` | Only with an iOS app | No | `1234567890-ios456.apps.googleusercontent.com` | Copied from Google Cloud Console (iOS client) |
| `GOOGLE_ANDROID_CLIENT_ID` | Only with an Android app | No | `1234567890-and789.apps.googleusercontent.com` | Copied from Google Cloud Console (Android client) |
| `APP_PUBLIC_URL` | In production | No | `https://api.example.com` | Your API's own address; you choose it |

You don't generate anything yourself: Google creates every value.

> [!NOTE]
> **Words on this page.** A **client** is your app's registration at Google: it says who is asking people to sign in. The **client ID** is the client's public name. The **client secret** is its password: only your server knows it. A **redirect URI** is the address on your API where Google sends people back after they sign in.

## How it works

1. Your website sends the browser to your API: `/v1/auth/google/start`.
2. Your API sends the browser on to Google, with your client ID.
3. The person signs in at Google and agrees to share their name and email address.
4. Google sends the browser back to your API's **redirect URI**, `/v1/auth/google/callback`, with a one-time code.
5. Your API trades the code for the person's identity, using the client secret. Then it signs them in (creating the account the first time) and sends them back to your website.

That's why Google needs to know your redirect URI in advance: it only sends people back to addresses you registered, so no one else can receive your users' codes.

## Your redirect URIs

Your API builds its redirect URI from `APP_PUBLIC_URL`:

| Environment | `APP_PUBLIC_URL` | Redirect URI to register at Google |
|---|---|---|
| Your computer | Empty (means `http://localhost:8080`) | `http://localhost:8080/v1/auth/google/callback` |
| Staging | `https://api.staging.example.com` | `https://api.staging.example.com/v1/auth/google/callback` |
| Production | `https://api.example.com` | `https://api.example.com/v1/auth/google/callback` |

Use your own addresses. They must match to the character: `http` or `https`, the host, the port, the path, and no slash at the end. If you changed `APP_ADDR` to another port on your computer, set `APP_PUBLIC_URL=http://localhost:<that port>` and register that.

## Step 1: Create a Google Cloud project

A project is a folder at Google that holds your app's sign-in settings.

<div class="steps">

1. **Sign in to Google Cloud Console**

   Open [console.cloud.google.com](https://console.cloud.google.com) and sign in with a Google account. For a company app, use a company account, so the project doesn't depend on one person. The first time, accept the terms of service. No billing account or card is needed for sign-in.

2. **Create the project**

   Click the project picker at the top of the page (it may say **Select a project**) → **New project**. Enter a **Project name**, such as `acme`. Leave **Location** as it is, or choose your organisation. Click **Create**.

3. **Select it**

   When it's ready, open the project picker again and choose your new project. Check that its name shows at the top before continuing: everything below goes into the selected project.

</div>

One project can hold the clients for all your environments. Some teams prefer a separate project per environment, so production settings can't be changed by accident while testing.

## Step 2: Set up the consent screen

The consent screen is the page Google shows people when they sign in: your app's name, logo and the information it asks for.

<div class="steps">

1. **Open Google Auth Platform**

   In the menu (☰) → **APIs & Services** → **OAuth consent screen**. This opens **Google Auth Platform**. Click **Get started**.

   You don't need to enable any API in **APIs & Services → Library** for sign-in.

2. **App information**

   **App name**: what people will see, such as `Acme`. **User support email**: an address people can contact, from your account's addresses or a Google Group you manage. Click **Next**.

3. **Audience**

   Choose **External**: anyone with a Google account can sign in. (**Internal** is only for apps used inside your own Google Workspace organisation.) Click **Next**.

4. **Contact information**

   Your email address, where Google tells you about changes to your project. Click **Next**, tick the agreement, and click **Create**.

5. **Branding**

   Open **Branding** in the left menu. Fill in **Application home page**, **Application privacy policy link** and **Application terms of service link**. Under **Authorized domains**, click **Add domain** and enter your domain, such as `example.com`. Click **Save**.

   A logo is optional. Adding one means Google reviews your branding before the logo appears.

6. **Data access**

   Nothing to add. Your API only asks for `openid`, `email` and `profile`: the person's Google ID, email address and name. Google treats these as basic information, so no review is needed.

7. **Test users, while you build**

   Open **Audience**. The **Publishing status** is **Testing**: only people listed under **Test users** can sign in. Click **Add users** and add your own Google address and your team's.

</div>

When you're ready for everyone, come back to **Audience** and click **Publish app**. With only basic information, it's published straight away without a review.

## Step 3: Create the web client

This is the client your API uses. Android apps use it too.

<div class="steps">

1. **Start a new client**

   In Google Auth Platform, open **Clients** → **Create client**.

2. **Application type**

   Choose **Web application**. Your API is the web application here: it's the part that talks to Google.

3. **Name**

   Only you see it, such as `acme API`.

4. **Authorized JavaScript origins**

   Leave empty. The sign-in flow on this page runs through your API's redirects, which don't use JavaScript origins. You only need them if your website shows Google's own JavaScript sign-in button and sends the result to your API itself, instead of linking to `/v1/auth/google/start`.

5. **Authorized redirect URIs**

   Click **Add URI** once per environment and paste the redirect URIs from [the table above](#your-redirect-uris), such as:

   ```text
   http://localhost:8080/v1/auth/google/callback
   https://api.example.com/v1/auth/google/callback
   ```

   Google accepts `http` only for `localhost`.

6. **Create**

   Click **Create**. A window shows the **Client ID** and **Client secret**.

7. **Copy both values now**

   Copy the client secret straight away, or click **Download JSON**: Google shows new secrets only at this moment. Treat it like a password. If you lose it, open the client, click **Add secret**, switch your app to the new one, then delete the old one.

</div>

## Step 4: Put the values in your app

On your computer, open `.env` in your app's folder and fill in:

```bash
GOOGLE_CLIENT_ID=1234567890-abc123.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=GOCSPX-your-secret
# APP_PUBLIC_URL stays empty on your computer: it means http://localhost:8080
```

In production, add the same names in your hosting provider's secret settings, plus your API's address:

```bash
GOOGLE_CLIENT_ID=1234567890-abc123.apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=GOCSPX-your-secret   # or GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google_client_secret
APP_PUBLIC_URL=https://api.example.com
```

Restart the app (stop `aps dev` with Ctrl+C and start it again).

## Step 5: Try it on your computer

<div class="steps">

1. **Check it's on**

   The **Sign-in methods** list that `aps dev` prints at start shows `✓ Google sign-in`. Or run `go run ./cmd/api auth-providers`.

2. **Sign in**

   In Chrome or Firefox, open:

   ```text
   http://localhost:8080/v1/auth/google/start
   ```

   Use `localhost`, not `127.0.0.1`: your API checks that the browser comes back to the same address it started on.

3. **Choose your Google account**

   Pick a test user from step 2. Google asks whether to share your name and email with your app.

4. **Arrive back signed in**

   Google returns you to your API, which signs you in with a session cookie and opens `http://localhost:8080/docs`. To see the account:

   ```text
   http://localhost:8080/v1/auth/me
   ```

   It shows your email address, with `"has_password": false`: accounts created with Google have no password until the person sets one with "forgot password".

</div>

From your own website, link to the start address with where to go afterwards: `/v1/auth/google/start?return_to=https://app.example.com/after-login`. That address must be your API itself or one of `APP_CORS_ORIGINS`.

If the email address already has an account, signing in with Google links to it, because Google has verified the address. If that account's email was never verified, its password is removed first, so whoever registered the address without owning it can't sign in anymore.

## Step 6 (optional): Your iOS app

<div class="steps">

1. **Create an iOS client**

   **Clients** → **Create client** → **Application type**: **iOS**.

2. **Bundle ID**

   Your app's bundle identifier, from Xcode → your target → **Signing & Capabilities**, such as `com.example.app`. **App Store ID** and **Team ID** are optional.

3. **Copy the client ID**

   Click **Create** and copy the **Client ID** to `GOOGLE_IOS_CLIENT_ID`. The **iOS URL scheme** shown with it goes into your app's `Info.plist`, as Google's Sign-In SDK instructions describe.

4. **Send the token to your API**

   In the app, get a one-time value from `POST /v1/auth/google/nonce`, pass it to Google Sign-In, and send the resulting ID token and the same value to `POST /v1/auth/google/token` with `"transport": "bearer"`. Your API answers with a session token; store it in the Keychain.

</div>

There's no client secret for iOS: a secret inside an app could be extracted by anyone who downloads it.

## Step 7 (optional): Your Android app

<div class="steps">

1. **Find your signing fingerprints**

   Android clients are tied to the key that signs your app. You need the **SHA-1** fingerprint of each key: debug builds from your computer, your upload key, and Google Play's app signing key are all different.

   - Debug: `keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android -keypass android`, then the `SHA1:` line. Or run `./gradlew signingReport` in your Android project.
   - Google Play: [Play Console](https://play.google.com/console) → your app → **Test and release** → **App integrity** → **App signing** → the SHA-1 of the **App signing key certificate** and of the **Upload key certificate**.

2. **Create an Android client per key**

   **Clients** → **Create client** → **Android**. **Package name**: your app's `applicationId` from `app/build.gradle`, such as `com.example.app`. **SHA-1 certificate fingerprint**: one from step 1. Click **Create**. Repeat for each key.

3. **Set the variable**

   Copy one Android **Client ID** to `GOOGLE_ANDROID_CLIENT_ID`.

4. **Use the web client ID in the app**

   In Android's Credential Manager, set the **server client ID** to your **web** client ID (`GOOGLE_CLIENT_ID`): Android issues ID tokens for it. Get a one-time value from `POST /v1/auth/google/nonce`, pass it as the nonce, and send the ID token and the value to `POST /v1/auth/google/token` with `"transport": "bearer"`.

</div>

## Check it works

- `go run ./cmd/api auth-providers` shows `✓ Google sign-in` (and the iOS and Android lines if you set them).
- An administrator can see the same with `GET /ops/auth/providers`. It never shows the values.
- Signing in through `/v1/auth/google/start` ends back at your site, signed in.

## If something goes wrong

| What you see | What it means | Fix |
|---|---|---|
| The app won't start: `GOOGLE_CLIENT_SECRET is required with GOOGLE_CLIENT_ID` | Only half of the web client is set | Add the secret, or empty the client ID to turn Google off |
| The app won't start: `GOOGLE_CLIENT_ID is required with GOOGLE_CLIENT_SECRET, GOOGLE_IOS_CLIENT_ID or GOOGLE_ANDROID_CLIENT_ID` | A mobile client ID or secret is set without the web client | Set `GOOGLE_CLIENT_ID`; mobile sign-in needs it too |
| The app won't start: `APP_PUBLIC_URL is required with Google or Apple sign-in` | Production, with no public address | Set `APP_PUBLIC_URL=https://api.example.com` |
| The app won't start: `APP_PUBLIC_URL … must use https in production` or `must be a scheme and host` | Wrong format | Use `https://` and the host only: no path, no slash at the end |
| Google shows `Error 400: redirect_uri_mismatch` | The redirect URI isn't registered exactly as your API sends it | Google shows the address it received: add exactly that under **Authorized redirect URIs**. Changes can take a few minutes |
| Google shows `Access blocked: … has not completed the Google verification process` | The consent screen is in **Testing** and your account isn't a test user | Add your address under **Audience → Test users**, or publish the app |
| Google shows `Error 401: invalid_client` | The client ID doesn't exist or was deleted | Check `GOOGLE_CLIENT_ID` is copied whole, from the right project |
| Back on your site with `#error=…` in the address | Sign-in didn't finish | The code after `#error=` says why. Check the app's log for the same request |
| Signed in at Google, but not signed in to your API on your computer | You started on `127.0.0.1` and came back on `localhost`, or the other way round | Start at `http://localhost:8080/v1/auth/google/start` |
| Android: `DEVELOPER_ERROR`, or no accounts offered | The Android client's package name or SHA-1 doesn't match the build | Add an Android client for the key that signed this build; use the web client ID as server client ID |
