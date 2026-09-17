# 7. Phone-code sign-in

Many of Shelfie's readers use the mobile app and forget passwords. This chapter lets a reader confirm a phone number and then sign in with a 6-digit code texted to it. The code check is Shelfie's; the session is sign-in's, so a phone sign-in gets everything a password sign-in gets: bans, the second factor, chapter 6's `BeforeLogin` hook, rate limits, the audit log, and a cookie or a bearer token ([adding a sign-in method](../../guides/adding-a-sign-in-method.md)).

> [!NOTE]
> Shelfie has no SMS provider. In development, codes are written to the log; production answers 503 `sms_unavailable` until you put your provider behind the `Sender` interface. The tests use a fake sender.

## 1. The routes

| Route | Who | Does |
|---|---|---|
| `PUT /v1/phone` | A signed-in reader | Sets their number and texts a code to confirm it |
| `POST /v1/phone/confirm` | A signed-in reader | Confirms the number with that code |
| `POST /v1/phone-sign-in/code` | Anyone | Texts a sign-in code when an account confirmed the number; always 202 |
| `POST /v1/phone-sign-in` | Anyone | Checks the code and signs the reader in, answering as `POST /v1/auth/login` |

<!-- include examples/apps/shelfie/internal/modules/phonelogin/delivery/routes.go#routes -->

## 2. The module takes the authenticator

`phonelogin` needs sign-in to create the session, so its `Module` takes the `*authhttp.Authenticator` and a sender:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/module.go#module -->

`main.go` passes the authenticator it gives to `gorbital.WithAuth`:

<!-- include examples/apps/shelfie/cmd/api/main.go#main -->

A module whose `Module` takes arguments isn't in `modules.All()` (`orb gen modules` lists `func Module()` only), so `main.go` adds it on its own line. `gorbital` doesn't import `authhttp` and `Deps` has no sign-in field: the dependency is visible in `main.go` instead of looked up ([ADR-0083](../../adr/0083-modules-stack-migrations-and-ejection.md#phase-6-implementation-notes-sign-in-options-hooks-and-custom-methods-2026-09-17)).

The sender in development:

<!-- include examples/apps/shelfie/cmd/api/signin.go#sms-sender -->

<!-- include examples/apps/shelfie/internal/modules/phonelogin/sender.go#log-sender -->

## 3. What the module verifies

`SignIn` checks nothing about phones: it trusts that the module proved who is signing in. So `phonelogin` does, before calling it:

- **The number is confirmed by the account.** A code goes only to a number its reader confirmed with an earlier code, and one confirmed number belongs to one account (a unique index).
- **Codes are single-use, short-lived and bounded.** Each works once, for 10 minutes and 5 guesses; a number receives at most one code a minute and five an hour, so at most 25 guesses an hour whoever asks. Only the code's SHA-256 is stored, and it is compared in constant time. The routes add `guard.RateLimit` per client address.
- **Nothing reveals whose number it is.** `POST /v1/phone-sign-in/code` answers 202 after the same minimum time whether or not an account confirmed the number:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/usecase/send_sign_in_code.go#send-sign-in-code -->

and a wrong code, a used one and a number without one all answer 401 `invalid_phone_code`:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/usecase/verify_sign_in_code.go#verify-sign-in-code -->

- **The user ID comes from the verified code, never from the request.**

## 4. Signing in

The handler hands the verified reader to sign-in and returns its answer as it is:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/delivery/sign_in.go#sign-in -->

[`SignIn`](../../methods/gorbital-authhttp.md#Authenticator.SignIn) applies, in order, what `POST /v1/auth/login` applies after a correct password:

| Check | Answer |
|---|---|
| The account's sign-in limits (`auth_login`, `auth_login_address`) | 429 `too_many_attempts` |
| The email address isn't verified | 403 `email_not_verified` |
| Two-factor authentication is on | 202 with `mfa.challenge_token`; finish with `POST /v1/auth/login/mfa` |
| An operator banned the account | 403 `account_banned` |
| Chapter 6's `BeforeLogin`: the reader is suspended | 403 `reader_suspended` |
| Otherwise | 200 with the session: `__Host-session` cookie, or `token` for `"transport": "bearer"` |

The audit log records `auth.login.succeeded` with `method: phone_code`, and hooks see `Method` `phone_code`. When the sign-in continues with a second factor, the challenge token carries the method, so the event after `POST /v1/auth/login/mfa` names it too.

## 5. Test it

The tests use a sender that records codes instead of texting them:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/phonelogin_test.go#fake-sms -->

Ada confirms her number, then signs in on a new phone; a number nobody confirmed gets the same answer and no text, and a code works once:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/phonelogin_test.go#phone-sign-in -->

With an authenticator app on, the phone code is only the first factor:

<!-- include examples/apps/shelfie/internal/modules/phonelogin/phonelogin_test.go#phone-sign-in-mfa -->

## Next

[8. Book clubs](08-book-clubs.md): clubs as organisations, a reading list generated with `orb gen module --org`, `guard.OrgMember` and row-level security.
