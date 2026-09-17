# Set up sign-in

Your app can already sign people in with an email address and a password. It can also offer authenticator apps, passkeys, Google, Apple and GitHub. Each of those needs a few values that belong to you: a key you generate yourself, or IDs and secrets from Google's, Apple's or GitHub's developer websites.

This section walks through every value, click by click. You don't need to have used those websites before.

> [!NOTE]
> gorbital never owns or sees these values. They go into your app's environment, and only your app uses them.

## What you can set up

| Sign-in method | What people use | What you need | Cost | Guide |
|---|---|---|---|---|
| Email and password | Their email address and a password | Nothing on your computer; an email provider in production | Free tiers exist | [Email sending](email.md) |
| Authenticator apps | A 6-digit code from an app such as Google Authenticator | One key you generate in a terminal | Free | [Encryption key](encryption-key.md) |
| Passkeys | Face ID, Touch ID, Windows Hello or their phone | Your domain name | Free | [Passkeys](passkeys.md) |
| Passkeys in your apps | The same passkeys inside your iOS or Android app | Your app's IDs | Apple needs a paid membership | [Passkeys in mobile apps](passkeys-mobile.md) |
| Google | Their Google account | A Google account | Free | [Google sign-in](google.md) |
| Apple | Their Apple Account | An Apple Developer Program membership | Paid, yearly | [Apple sign-in](apple.md) |
| GitHub | Their GitHub account, on your website | A GitHub account | Free | [GitHub sign-in](github.md) |

You don't need all of them. Start with the ones your users expect.

## Words you'll meet

| Word | What it means here |
|---|---|
| Environment variable | A named value your app reads when it starts, written as `NAME=value`, such as `GOOGLE_CLIENT_ID=1234-abc.apps.googleusercontent.com` |
| `.env` | A file in your app's folder that holds environment variables on your computer. Git ignores it, so it never gets committed |
| Secret | A value that lets someone act as your app, such as a client secret or a private key. Treat it like a password: never in git, chat or screenshots |
| Secret store | Where production secrets live: the environment or secrets settings of your hosting provider |
| Client ID | The public name of your app at Google or Apple. Not a secret |
| Redirect URI, return URL | The address on your API that Google or Apple sends people back to after they sign in |
| Domain | Your website's name, such as `example.com` |

## Where the values go

1. **On your computer:** open `.env` in your app's folder and fill in the line for the value, such as `GOOGLE_CLIENT_ID=…`. Every name is already listed with a comment in `.env.example`, and `orb dev` creates `.env` from it the first time.
2. **In production:** add the same names in your hosting provider's environment or secrets settings. Use different values from the ones on your computer.
3. **Restart the app** after changing a value.

A few rules keep this safe:

- **Empty means off.** Leave a method's values empty and that method is off; the rest of the app keeps working.
- **Half-filled means stop.** If you fill in part of a method, such as a Google client ID without its secret, the app refuses to start and names the variable to fix. A method can't silently stay broken.
- **Secrets can come from files.** For a secret such as `GOOGLE_CLIENT_SECRET`, you can set `GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google` instead, pointing to a file that holds it. Hosting platforms often mount secrets this way.

## See what's turned on

In your app's folder, run:

```bash
go run ./cmd/api auth-providers
```

```text
Sign-in methods
  ✓ Email and password
  ✓ Authenticator apps (2FA)
  ✓ Passkeys in browsers      RP ID localhost; origins http://localhost:8080, http://localhost:3000
  – Passkeys in iOS apps      set WEBAUTHN_APPLE_APP_IDS in .env  AUTH_PROVIDERS.md#passkeys-in-ios-apps
  – Passkeys in Android apps  set WEBAUTHN_ANDROID_APPS in .env   AUTH_PROVIDERS.md#passkeys-in-android-apps
```

A tick means the method is on. A dash means it's off, and shows what to set. In an app created with `orb` v0.1, `orb dev` prints the same list each time the app starts. In an app on [`gorbital.Main`](../guides/main-go.md), sign-in is the library's `authhttp` and the command is the same; at start the app logs one `sign-in method` line per method instead, with `method` and `configured`.

## A good order

Want the whole list at once, with which values you generate, copy or download? See [Every key and credential](all-keys.md).

1. [Encryption key](encryption-key.md): two minutes, and production needs it.
2. [Email sending](email.md): sign-up codes need it once real people use your app.
3. [Passkeys](passkeys.md): quick, and the most secure way to sign in.
4. [Google sign-in](google.md), then [Apple sign-in](apple.md). If your iOS app offers Google sign-in, the App Store generally requires Apple sign-in too. [GitHub sign-in](github.md) if your users are developers.
5. [Go-live checklist](go-live.md) before real users arrive.

Every app also contains `AUTH_PROVIDERS.md`, with the same steps in one file next to your code.

These pages are about values in the environment. To change how sign-in behaves in code in an app on `gorbital.Main`, such as closing sign-up or asking for a display name at registration, see [Configuring sign-in](../guides/configuring-sign-in.md).
