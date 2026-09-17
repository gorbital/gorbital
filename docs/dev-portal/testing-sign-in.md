# Testing sign-in

Check that each sign-in method works with the keys in your `.env` before anyone signs in: Google, Apple and GitHub against the real provider, passkeys with a real authenticator, authenticator apps with your phone, and email through your delivery. Tests create no account, identity link, session or audit event, and write nothing to the database. Development only.

It's the **Test sign-in** tab of the [Authentication](authentication.md) screen (`/auth?tab=tests`, `&method=google` to jump to one). The [Routes](routes.md) screen links each `/v1/auth/*` route to it. Decided in [ADR-0087](../adr/0087-testing-sign-in-from-the-dev-portal.md).

The app needs sign-in from the library (`gorbital.WithAuth(authhttp.New())`) and the dev console, which `orb dev` turns on. An app created with orb v0.1, whose sign-in is generated code, doesn't have the tests: the tab says so.

## What you see

A card per method: Google, Apple, GitHub, Passkeys, Authenticator apps and Email. Each lists **checks** made from the configuration alone, without the network: every check has a status (ok, warning, failed, skipped), what was found, the fix, the variables involved and a link to the screen where you fix it ([Environment](environment.md), [Tunnel](tunnel.md), [Mail](mail.md) or the guide).

| Method | What the checks look at |
|---|---|
| Google | The client ID and secret have Google's shape; the iOS and Android client IDs; `APP_PUBLIC_URL` (http only on `localhost`); the callback URL to register; that a `localhost` `APP_PUBLIC_URL` names the port the app listens on (`APP_ADDR`); a quick tunnel's changing address |
| Apple | The Team ID and Key ID; that the `.p8` key loads **and signs a client secret**, as every Apple request does; the Services ID, distinct from the bundle IDs; that `APP_PUBLIC_URL` is https on a real domain, which Apple requires |
| GitHub | An OAuth app's client ID (a GitHub App's gets a warning: it ignores the scopes gorbital asks for), the secret's shape, the callback URL and port |
| Passkeys | The RP ID is a domain (not an IP, not a public suffix such as `github.io`); each origin is https (http only on `localhost`) and on the RP ID; whether the live test can run on `APP_PUBLIC_URL` |
| Authenticator apps | `AUTH_ENCRYPTION_KEYS` encrypts and decrypts a secret |
| Email | `MAIL_DELIVERY`: `devmail` and `mailpit` catch everything; `provider` sends to real addresses |

## Check

**Check** on Google, Apple and GitHub goes online:

| Check | How |
|---|---|
| `provider_reachable` | Requests the provider's token endpoint |
| `clock_skew` | Compares the provider's `Date` with this computer's clock: a warning from 10 seconds, a failure from a minute, where ID tokens start to look issued in the future and Apple refuses client secrets |
| `client_credentials` | Sends your client ID and secret (for Apple, a client secret signed with your key) to the token endpoint with a made-up code. A provider that accepts the client refuses only the code (`invalid_grant`): that's a pass. `invalid_client` means the ID, secret, key or team don't belong together |

For other methods, Check reloads the offline checks.

## Test now: Google, Apple and GitHub

**Test now** opens a window on the provider's sign-in page, with your client and **your app's real callback URL**, so what you registered with the provider is what gets tested. Sign in with any account. The provider returns to `/v1/auth/{provider}/callback` as it does for your users; the app recognises the test, exchanges the code with your secret and verifies the identity exactly as sign-in does (PKCE, signature against the provider's keys, issuer, audience, nonce, age; GitHub's user and email API), then sends the window to the portal, which shows the result and closes it.

A pass shows what the provider said about the account (its ID at the provider, email, whether the email is verified, name, the client ID the token was issued for) with warnings when sign-in would refuse a new account: no email, or an email the provider hasn't verified. Tokens are never shown, kept or logged.

A failure names the cause and the fix:

| Code | Meaning |
|---|---|
| `redirect_uri_mismatch` | The callback URL shown on the card isn't registered for this client. Google shows its own error page instead of returning: when the window shows `Error 400: redirect_uri_mismatch`, the result never arrives; register the URL and test again |
| `invalid_client` | The provider doesn't accept the client ID and secret (Apple: the team, key, `.p8` file and Services ID) |
| `invalid_grant` | The code was refused: used, expired, or for another redirect URI |
| `access_denied` | You cancelled at the provider |
| `audience_mismatch`, `issuer_mismatch`, `nonce_mismatch`, `id_token_signature` | The ID token doesn't verify, and why |
| `token_expired`, `clock_skew` | The token's times don't fit this computer's clock: run Check |
| `provider_unreachable`, `github_api_error`, `provider_error` | The network, GitHub's API, or another answer from the provider, with its message (tokens removed) |
| `expired` | The window wasn't finished within 10 minutes |

**Apple** accepts only https return URLs on a real domain: on `localhost` the button is disabled and links to the [Tunnel](tunnel.md) screen. Start a named tunnel, apply its `.env` changes, register the Return URL it shows, and test again. Checking the key and client works on `localhost`.

### A mobile app's ID token

Google and Apple cards also verify an ID token your iOS or Android app got from the provider's SDK: paste it with the nonce you sent (for Apple, the nonce **before** hashing, as your app sends it to `POST /v1/auth/apple/token`). It's checked like the token endpoints check it: signature, issuer, an audience among your client IDs (`GOOGLE_IOS_CLIENT_ID`, `APPLE_BUNDLE_IDS`…), age and nonce. When an Apple token carries the raw nonce instead of its SHA-256, the fix says so. The only thing skipped is the nonce store, since this nonce didn't come from `POST /v1/auth/{provider}/nonce`.

## Test now: passkeys

The browser must be on an origin in `WEBAUTHN_ORIGINS`, which the portal (`127.0.0.1:3100`) never is, so the test opens a page the app serves on `APP_PUBLIC_URL` (`/_signin-test/passkey`). It creates a passkey for a throwaway user and signs in with it, both verified as sign-in verifies them (a discoverable credential with user verification). The server keeps nothing; your password manager does keep the passkey, named **gorbital passkey test (safe to delete)**, unless the browser supports forgetting it (the page asks it to).

| Code | Meaning |
|---|---|
| `rp_id_mismatch` | The browser refused `WEBAUTHN_RP_ID` on this page's origin: the RP ID must be the page's host or a parent domain |
| `origin_not_allowed` | The origin the browser reported isn't in `WEBAUTHN_ORIGINS` |
| `passkey_cancelled` | The prompt was cancelled or timed out; on `127.0.0.1` browsers refuse passkeys, use `localhost` |
| `passkey_unsupported` | No passkey support in this browser or device |
| `passkey_invalid` | Verification failed, for example without user verification |

When `APP_PUBLIC_URL`'s origin isn't in `WEBAUTHN_ORIGINS` (passkeys used only by a frontend on another port), the live test can't run here and the card says so; the offline checks still apply. On a tunnel's hostname, the page is reachable through the tunnel, but does nothing without the single-use ticket the portal gets from the dev console.

## Test now: authenticator apps and email

**Authenticator apps**: the card shows a QR code and secret for a throwaway entry (named after your app, with "test"). Scan it, type the code: a pass, `invalid_code`, or `clock_drift` with how many seconds your phone and this computer disagree (sign-in accepts 30 seconds either way). Five attempts; the secret lives 10 minutes in the app's memory. Delete the entry from the app afterwards.

**Email**: type an address and send the test message through the configured delivery (the same as Mail's preview send). "Accepted" means the delivery took it; with `devmail` or `mailpit` it's in the [Mail](mail.md) inbox.

## Where it comes from

The app's dev console, through the portal's proxy, which adds the console token:

| Endpoint | |
|---|---|
| `GET /_dev/auth/test` | Methods and their offline checks |
| `POST /_dev/auth/test/{google\|apple\|github}/check` | Network checks |
| `POST /_dev/auth/test/{google\|apple\|github\|passkeys}/start` `{"result_url"}` | Starts a test; answers the URL to open |
| `GET /_dev/auth/test/results/{id}` | A test's result |
| `POST /_dev/auth/test/{google\|apple}/id-token` `{"id_token","nonce"}` | Verifies a pasted ID token |
| `POST /_dev/auth/test/totp/start`, `…/totp/verify` `{"id","code"}` | Authenticator app test |
| `POST /_dev/mail/preview/send?name=test&to=` | Test email |

Codes, fields and limits are in [ADR-0087](../adr/0087-testing-sign-in-from-the-dev-portal.md#endpoints).

## Security

- Tests exist only while the dev console does: `APP_ENV=development` with `DEV_CONSOLE_TOKEN`, which production refuses. Their endpoints refuse requests without the token, from another machine, with a public `Host`, or through a proxy or tunnel.
- A test's state, nonce, ticket or secret is random, kept only in the app's memory, single use and short-lived. A return whose state isn't a test's goes to sign-in unchanged; a test's is answered before sign-in sees it, and a replay lands on the same result.
- Nothing is written: no account, identity, session, cookie, OAuth state, nonce, passkey, TOTP secret or audit event. The app logs `sign-in test finished` with the method and outcome.
- The window is sent back only to an address on `localhost`, `127.0.0.1` or `[::1]`, and the result page talks only to the portal's own origin.
