# Passkeys in mobile apps

Your iOS and Android apps can use the same passkeys as your website: someone who created a passkey in their browser can sign in to your app with it, and the other way round. For that, the phone must trust that your app and your domain belong together. You prove it with a small file on your domain, which your API builds for you from one value per platform.

Set up [passkeys for browsers](passkeys.md) first: apps use the same `WEBAUTHN_RP_ID`. The examples below use `example.com` as that domain.

| Variable | Secret? | What it is | Example |
|---|---|---|---|
| `WEBAUTHN_APPLE_APP_IDS` | No | Your iOS apps, as `TEAMID.bundle.id`, separated by commas | `ABCDE12345.com.example.app` |
| `WEBAUTHN_ANDROID_APPS` | No | Your Android apps, as `package.name=SHA256:FINGERPRINT`, separated by commas | `com.example.app=SHA256:AB:CD:…:EF` |

## iOS apps

You need an [Apple Developer Program](https://developer.apple.com/programs/) membership and your app's project open in Xcode.

<div class="steps">

1. **Find your Team ID**

   Sign in at [developer.apple.com/account](https://developer.apple.com/account) and open **Membership details**. The **Team ID** is 10 letters and digits, such as `ABCDE12345`. It identifies your developer account.

2. **Find your Bundle ID**

   In Xcode, select your project, then your app's target, then **Signing & Capabilities**. The **Bundle Identifier** looks like `com.example.app`. It identifies your app.

3. **Set the variable**

   Join the two with a dot:

   ```bash
   WEBAUTHN_APPLE_APP_IDS=ABCDE12345.com.example.app
   ```

   For several apps, separate the entries with commas.

4. **Add Associated Domains in Xcode**

   Still in **Signing & Capabilities**, click **+ Capability**, choose **Associated Domains**, and add `webcredentials:example.com`, using your own `WEBAUTHN_RP_ID`. Xcode turns the capability on for your app at Apple.

5. **Make the file reachable on your domain**

   Your API now serves `/.well-known/apple-app-site-association`. Apple downloads it from `https://example.com/.well-known/apple-app-site-association`, over https and without redirects. If a separate website serves `example.com` rather than your API, have that website forward this one path to your API.

</div>

### Check it works

```bash
curl -i https://example.com/.well-known/apple-app-site-association
```

You should see `200`, `Content-Type: application/json`, and your app ID under `"webcredentials"`.

Apple copies the file into its own cache, so a change can take a while to reach phones. To see what phones see, open `https://app-site-association.cdn-apple.com/a/v1/example.com`. While developing, you can skip the cache: add `?mode=developer` to the entitlement (`webcredentials:example.com?mode=developer`), and on the test iPhone turn on **Settings → Developer → Associated Domains Development**.

## Android apps

<div class="steps">

1. **Find your package name**

   It's the `applicationId` in your app's `app/build.gradle` or `app/build.gradle.kts` file, such as `com.example.app`.

2. **Collect your signing fingerprints**

   Every Android build is signed with a key, and a fingerprint is a short code that identifies that key. Android only lets your app use the passkeys when the fingerprint matches, so you need the **SHA-256** fingerprint of every key that signs your app. Most apps have up to three:

   - **App signing key**, used for installs from Google Play: [Play Console](https://play.google.com/console) → your app → **Test and release** → **App integrity** → **App signing** → **App signing key certificate** → **SHA-256 certificate fingerprint**.
   - **Upload key**, used for builds you upload or share for testing: the same page → **Upload key certificate** → SHA-256.
   - **Debug key**, used for builds from your computer. Run:

     ```bash
     keytool -list -v -keystore ~/.android/debug.keystore -alias androiddebugkey -storepass android -keypass android | grep SHA256
     ```

     Or run `./gradlew signingReport` in your Android project.

3. **Set the variable**

   Write the package name, then `=`, then the fingerprints joined with `+`:

   ```bash
   WEBAUTHN_ANDROID_APPS=com.example.app=SHA256:AB:CD:…:EF+SHA256:12:34:…:56
   ```

   For several apps, separate the entries with commas.

4. **Make the file reachable on your domain**

   Your API now serves `/.well-known/assetlinks.json`. Google checks it at `https://example.com/.well-known/assetlinks.json`. If a separate website serves that domain, have it forward this path to your API.

</div>

### Check it works

```bash
curl -s https://example.com/.well-known/assetlinks.json
```

It should list your package name with `delegate_permission/common.get_login_creds` and your fingerprints. To see what Google sees:

```bash
curl -s "https://digitalassetlinks.googleapis.com/v1/statements:list?source.web.site=https://example.com&relation=delegate_permission/common.get_login_creds"
```

If the passkey prompt never appears in your Android app, the cause is almost always a missing fingerprint: the debug, upload and app signing keys are all different.
