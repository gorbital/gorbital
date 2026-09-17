# GitHub sign-in

Let people sign in with their GitHub account on your website. This page starts from a GitHub account and nothing else. It takes about 10 minutes, and GitHub charges nothing for it.

## What you'll end up with

| Variable | Required? | Secret? | Looks like | How you get it |
|---|---|---|---|---|
| `GITHUB_CLIENT_ID` | Yes, to turn GitHub on | No | `Ov23liAbCdEf12345678` | Copied from your OAuth app's page on GitHub |
| `GITHUB_CLIENT_SECRET` | Yes, with the client ID | **Yes** | 40 hexadecimal characters | Generated on the same page; shown once |
| `APP_PUBLIC_URL` | In production | No | `https://api.example.com` | Your API's own address; you choose it |
| `AUTH_DEFAULT_RETURN_TO` | In production | No | `https://app.example.com/signed-in` | A page of your website; you choose it ([after signing in](#after-signing-in)) |

You don't generate anything yourself: GitHub creates both credentials.

> [!NOTE]
> **Words on this page.** An **OAuth app** is your app's registration at GitHub: it says who is asking people to sign in. The **client ID** is its public name. The **client secret** is its password: only your server knows it. The **callback URL** is the address on your API where GitHub sends people back after they approve.

> [!WARNING]
> **Web only.** GitHub has no sign-in SDK for iOS or Android apps and no ID tokens, so there's no native GitHub sign-in like Google's or Apple's. A mobile app that wants GitHub opens the web flow in a browser tab.

## How it works

1. Your website sends the browser to your API: `/v1/auth/github/start`.
2. Your API sends the browser on to GitHub, with your client ID.
3. The person signs in at GitHub and approves sharing their profile and email addresses.
4. GitHub sends the browser back to your API's **callback URL**, `/v1/auth/github/callback`, with a one-time code.
5. Your API trades the code for a short-lived access token, using the client secret, and asks GitHub for the person's numeric ID, name and **primary** email address. It forgets the token. Then it signs them in (creating the account the first time) and sends them back to your website.

Your API asks only for `read:user` and `user:email`, so it can't see repositories or act on GitHub for the person.

## What's different from Google and Apple

GitHub doesn't run anyone's mailbox. Google knows a Gmail address belongs to its account holder; GitHub only knows the person once confirmed the address, possibly years ago with an employer's mailbox they no longer have. So your API trusts GitHub less:

| Situation | What happens |
|---|---|
| First GitHub sign-in, address has no account | A new account without a password. Its email address is **not verified**: the person verifies it with a code while signed in. Until then it can't be given roles or accept organisation invitations. It isn't deleted as unverified while GitHub is linked |
| The address already has an account | Nothing is linked, even for a Gmail address. The person returns to your site with `#error=social_link_required`: they sign in with their password and [link GitHub](#link-github-to-an-existing-account) |
| The GitHub account has no verified primary address | Refused with `#error=social_email_unverified` |
| Someone else later proves the address by email (a password reset, or verifying it) | They take the account over, and the GitHub link is removed with its sessions |
| The person renames their GitHub login or changes their email | Still the same account: your API remembers GitHub's numeric user ID |

## Your callback URLs

Your API builds its callback URL from `APP_PUBLIC_URL`:

| Environment | `APP_PUBLIC_URL` | Callback URL to register at GitHub |
|---|---|---|
| Your computer | Empty (means `http://localhost:8080`) | `http://localhost:8080/v1/auth/github/callback` |
| Staging | `https://api.staging.example.com` | `https://api.staging.example.com/v1/auth/github/callback` |
| Production | `https://api.example.com` | `https://api.example.com/v1/auth/github/callback` |

A GitHub OAuth app has exactly **one** callback URL, so create one OAuth app per environment. That also keeps your production secret off your computer.

## Step 1: Create the OAuth app

<div class="steps">

1. **Choose the owner**

   For a company app, create the OAuth app under your GitHub organisation, so it doesn't depend on one person's account: open the organisation → **Settings** → **Developer settings** (at the bottom of the left menu) → **OAuth Apps**. For a personal project: your picture (top right) → **Settings** → **Developer settings** → **OAuth Apps**.

2. **Start a new app**

   Click **New OAuth App** (or **Register a new application** if you have none yet).

   Choose **OAuth App**, not **GitHub App**: your API uses OAuth scopes, which GitHub Apps don't have.

3. **Application name**

   What people see on GitHub's approval page, such as `Acme`. Add the environment for the others, such as `Acme (development)`.

4. **Homepage URL**

   Your website, such as `https://example.com`. On your computer, `http://localhost:8080` works.

5. **Application description**

   Optional. People see it on the approval page.

6. **Authorization callback URL**

   The callback URL for this environment from [the table above](#your-callback-urls), such as:

   ```text
   http://localhost:8080/v1/auth/github/callback
   ```

   It must match your API's to the character: `http` or `https`, the host, the port and the path, no slash at the end.

7. **Enable Device Flow**

   Leave it unticked. Click **Register application**.

</div>

## Step 2: Copy the client ID and create a secret

<div class="steps">

1. **Copy the client ID**

   The app's page shows the **Client ID**. Copy it.

2. **Generate a client secret**

   Click **Generate a new client secret**. GitHub may ask for your password or two-factor code first.

3. **Copy the secret now**

   GitHub shows the secret only this once. Treat it like a password. If you lose it, generate another, switch your app to the new one, then delete the old one with **Delete**.

4. **Add a logo (optional)**

   Under **Application logo**, upload your app's icon. People see it when they approve.

</div>

## Step 3: Put the values in your app

On your computer, open `.env` in your app's folder and fill in:

```bash
GITHUB_CLIENT_ID=Ov23liAbCdEf12345678
GITHUB_CLIENT_SECRET=0123456789abcdef0123456789abcdef01234567
# APP_PUBLIC_URL and AUTH_DEFAULT_RETURN_TO stay empty on your computer
```

In production, add the values of your production OAuth app in your hosting provider's secret settings, plus your API's address and where sign-ins end:

```bash
GITHUB_CLIENT_ID=Ov23liXyZ98765432109
GITHUB_CLIENT_SECRET=<production secret>   # or GITHUB_CLIENT_SECRET_FILE=/run/secrets/github_client_secret
APP_PUBLIC_URL=https://api.example.com
AUTH_DEFAULT_RETURN_TO=https://app.example.com/signed-in
```

Restart the app (stop `orb dev` with Ctrl+C and start it again).

## Step 4: Try it on your computer

<div class="steps">

1. **Check it's on**

   Run `go run ./cmd/api auth-providers`: it shows `✓ GitHub sign-in` with its callback URL. In an app created with `orb` v0.1, the **Sign-in methods** list that `orb dev` prints at start shows it too; an app on `gorbital.Main` logs `sign-in method` with `method=github` and `configured=true` instead.

2. **Sign in**

   In Chrome or Firefox, open:

   ```text
   http://localhost:8080/v1/auth/github/start
   ```

   Use `localhost`, not `127.0.0.1`: your API checks that the browser comes back to the same address it started on.

3. **Approve**

   GitHub shows your app's name and asks to share your profile and email addresses. Click **Authorize**.

4. **Arrive back signed in**

   GitHub returns you to your API, which signs you in with a session cookie and opens `http://localhost:8080/docs`. To see the account:

   ```text
   http://localhost:8080/v1/auth/me
   ```

   It shows your GitHub primary address with `"email_verified": false` and `"has_password": false`.

</div>

From your own website, link to the start address with where to go afterwards: `/v1/auth/github/start?return_to=https://app.example.com/after-login`. That address must be your API itself or one of `APP_CORS_ORIGINS`.

## After signing in

The API sends the browser back to `return_to`:

| Result | Address |
|---|---|
| Signed in | `return_to`, with the session cookie set |
| The account has two-factor authentication | `return_to#mfa_challenge_token=…&methods=…`; finish with `POST /v1/auth/login/mfa` |
| Didn't work | `return_to#error=<code>`, such as `social_link_required` or `access_denied` (the person clicked **Cancel**) |

When your website doesn't pass `return_to`, or a sign-in fails before the API knows it (an expired sign-in), the browser goes to `AUTH_DEFAULT_RETURN_TO`. On your computer, empty means the API docs. In production it's required, and must be a page of your website on `APP_CORS_ORIGINS` or your API's own address, using https: the app refuses to start without it, because the API docs are off in production and people would land on a "not found" page. Choose a page that reads the part after `#`.

## Link GitHub to an existing account

GitHub never joins an existing account by itself. The account's owner links it while signed in:

<div class="steps">

1. **Start from your website**

   On the account settings page, ask for the password, then call your API **with the browser's cookies**:

   ```js
   const r = await fetch("https://api.example.com/v1/auth/github/link", {
     method: "POST",
     credentials: "include",
     headers: { "Content-Type": "application/json" },
     body: JSON.stringify({ password, return_to: "https://app.example.com/settings" }),
   });
   const { url } = await r.json();
   window.location = url;
   ```

   People without a password sign in again first (a sign-in less than 10 minutes old stands in for it).

2. **Approve at GitHub**

   The person approves as for a sign-in.

3. **Back on the settings page**

   GitHub is linked: `GET /v1/auth/identities` lists it, and the GitHub button signs in to this account. Nobody is signed in by the link itself. On failure the address ends with `#error=identity_in_use` (another account has this GitHub account), `#error=unauthenticated` (the person signed out meanwhile) or `#error=invalid_state` (started in another browser, or older than 10 minutes).

</div>

The request sets a short-lived cookie in the browser that must come back from GitHub, so a link someone sends can't attach their GitHub account to yours. That works when your website and API share a site, such as `app.example.com` and `api.example.com`. A website on a different site (`example.app` calling `api.example.com`) is refused by browsers that block third-party cookies; serve the settings page from the API's site instead.

To unlink: `DELETE /v1/auth/identities/{id}` with the password.

## Check it works

- `go run ./cmd/api auth-providers` shows `✓ GitHub sign-in`.
- An administrator can see the same with `GET /ops/auth/providers`. It never shows the values.
- Signing in through `/v1/auth/github/start` ends back at your site, signed in.

## If something goes wrong

| What you see | What it means | Fix |
|---|---|---|
| The app won't start: `GITHUB_CLIENT_SECRET is required with GITHUB_CLIENT_ID` | Only half of the OAuth app is set | Add the secret, or empty the client ID to turn GitHub off |
| The app won't start: `GITHUB_CLIENT_ID is required with GITHUB_CLIENT_SECRET` | A secret without its client ID | Add the client ID |
| The app won't start: `APP_PUBLIC_URL is required with Google, Apple or GitHub sign-in` | Production, with no public address | Set `APP_PUBLIC_URL=https://api.example.com` |
| The app won't start: `AUTH_DEFAULT_RETURN_TO is required …` | Production (or docs turned off), with no default page | Set `AUTH_DEFAULT_RETURN_TO` to a page of your website |
| The app won't start: `AUTH_DEFAULT_RETURN_TO … must be on APP_PUBLIC_URL or an origin in APP_CORS_ORIGINS` | The page is on another site | Use your website's origin and add it to `APP_CORS_ORIGINS` |
| GitHub shows `The redirect_uri is not associated with this application` | The OAuth app's callback URL isn't your API's | Set **Authorization callback URL** to `<APP_PUBLIC_URL>/v1/auth/github/callback` and click **Update application**, or use this environment's OAuth app |
| Back on your site with `#error=invalid_social_token`; the log says `incorrect_client_credentials` | The secret doesn't belong to the client ID, or was deleted | Generate a new secret for this OAuth app |
| `#error=invalid_social_token`; the log says `bad_verification_code` | The one-time code was used or expired | Start again |
| `#error=social_email_unverified` | The GitHub account's primary address isn't verified | At GitHub → **Settings** → **Emails**, verify the primary address (or make a verified one primary) |
| `#error=social_link_required` | That address already has an account | Sign in with the password and [link GitHub](#link-github-to-an-existing-account) |
| Signed in at GitHub, but not signed in to your API on your computer | You started on `127.0.0.1` and came back on `localhost`, or the other way round | Start at `http://localhost:8080/v1/auth/github/start` |
