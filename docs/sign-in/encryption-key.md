# Encryption key

Authenticator apps such as Google Authenticator, Microsoft Authenticator, 1Password and Authy give people a second step at sign-in: a 6-digit code that changes every 30 seconds. To check those codes, your app keeps a small secret for each person, and it encrypts every one of those secrets with a key only your app has. That key is `AUTH_ENCRYPTION_KEYS`.

| Variable | Secret? | What it is |
|---|---|---|
| `AUTH_ENCRYPTION_KEYS` | **Yes** | One or more named keys, such as `k1:q3Jm0…=`. The first one encrypts; every one listed can decrypt |

Production needs it: without it the app refuses to start there. Administrator roles (`platform_admin` and `ops_viewer`) always require a second factor.

## On your computer

Nothing to do. When the value is empty, `orb dev` generates a key for development and writes it to `.env`. It does so when `.env.example` lists `AUTH_ENCRYPTION_KEYS=`, as every Full app's does; in an app on `gorbital.Main`, add that line to `.env.example` if it isn't there.

## For production

<div class="steps">

1. **Open a terminal**

   On macOS, open the Terminal app. On Windows, open PowerShell. On Linux, open your terminal.

2. **Generate the key**

   On macOS, Linux or WSL:

   ```bash
   echo "k1:$(openssl rand -base64 32)"
   ```

   On Windows PowerShell 7:

   ```powershell
   "k1:" + [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
   ```

   It prints one line, such as `k1:q3Jm0xV…=`. `k1` is the key's name; the rest is 32 random bytes written as text.

3. **Store it as a secret**

   In your hosting provider's secrets settings, add `AUTH_ENCRYPTION_KEYS` with the whole line, including `k1:`. If your platform mounts secrets as files, set `AUTH_ENCRYPTION_KEYS_FILE` to the file's path instead. Don't reuse the key from your computer.

4. **Back it up**

   Save a copy in your password manager, as you would a database password. If every copy is lost, no one's authenticator app works until an operator runs `go run ./cmd/api reset-mfa <email>` for each person, and they set it up again.

</div>

## Check it works

In the production environment, `go run ./cmd/api auth-providers` shows `✓ Authenticator apps (2FA)`.

## Replace the key later

Replace the key if someone who shouldn't have it may have seen it, or on a schedule if your policies require it.

<div class="steps">

1. **Generate a second key**

   Create one exactly as above, but name it `k2`: `echo "k2:$(openssl rand -base64 32)"`.

2. **Put it first**

   On every instance of your app, set both keys with the new one first: `AUTH_ENCRYPTION_KEYS=k2:…,k1:…`. New secrets are now encrypted with `k2`, and old ones still open with `k1`.

3. **Re-encrypt everyone's secret**

   ```bash
   go run ./cmd/api rotate-auth-keys
   ```

4. **Remove the old key**

   Once the command finishes, set `AUTH_ENCRYPTION_KEYS=k2:…` on every instance.

</div>

> [!DONT]
> Don't remove the old key before `rotate-auth-keys` finishes. Secrets still encrypted with it would become unreadable, and those people would lose their authenticator app.
