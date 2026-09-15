# Passkeys

A passkey lets someone sign in with Face ID, Touch ID, Windows Hello, their phone or a security key, instead of typing a password. It can replace the password completely, or be the second step after one. Passkeys can't be phished: each one only works on the website it was created for.

Browsers need no account with Apple or Google for this. You tell your app two things: your domain, and the addresses of the web pages that use passkeys.

| Variable | Secret? | What it is | Example |
|---|---|---|---|
| `WEBAUTHN_RP_ID` | No | Your domain, which every passkey is tied to | `example.com` |
| `WEBAUTHN_ORIGINS` | No | The web addresses of the pages that use passkeys, separated by commas | `https://app.example.com` |

## On your computer

Nothing to set. Passkeys work on `localhost`. Open your app at **http://localhost:8080**, not `http://127.0.0.1:8080`: browsers don't allow passkeys on IP addresses.

## For production

<div class="steps">

1. **Choose your passkey domain**

   This is `WEBAUTHN_RP_ID`. RP stands for "relying party": the site people sign in to. Use the domain people see, usually without `www` or `app`, such as `example.com`. A passkey made for `example.com` also works on `app.example.com`, `www.example.com` and your mobile apps.

   Choose carefully. Every passkey is tied to this value, so changing it later means everyone has to add their passkeys again.

2. **List the pages that use passkeys**

   This is `WEBAUTHN_ORIGINS`: the addresses of your web frontends, as scheme and host only, with no path and no trailing slash. Separate several with commas. Each must be your passkey domain or one of its subdomains, and must start with `https://`.

3. **Set both values**

   In your production environment:

   ```bash
   WEBAUTHN_RP_ID=example.com
   WEBAUTHN_ORIGINS=https://example.com,https://app.example.com
   ```

4. **Let those pages call your API**

   Add the same addresses to `APP_CORS_ORIGINS`. Browsers block requests from a page to an API on another address unless the API allows it, and this is where you allow it.

</div>

If `WEBAUTHN_RP_ID` is empty in production, passkeys are turned off, and passkey requests answer 503 `passkeys_unavailable`.

## Check it works

- `go run ./cmd/api auth-providers` shows `✓ Passkeys in browsers` with your domain and addresses.
- When your frontend starts adding a passkey, `POST /v1/auth/passkeys/registration` returns options containing `"rp": {"id": "example.com"}`.

## If something goes wrong

| What you see | What it means |
|---|---|
| `SecurityError` in the browser's console | The page's address isn't on `WEBAUTHN_RP_ID`, doesn't use https, or is an IP address |
| A CORS error in the browser's console | The page's address is missing from `APP_CORS_ORIGINS` |
| 503 `passkeys_unavailable` | `WEBAUTHN_RP_ID` is empty in production |

Next, if you have an iOS or Android app: [Passkeys in mobile apps](passkeys-mobile.md).
