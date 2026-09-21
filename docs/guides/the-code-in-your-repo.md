# The code in your repo

`orb new` writes sign-in into `internal/modules/auth`, and with a tenancy it writes organisations into `internal/modules/orgs`. Both are your code from the moment the command finishes: every handler, every use case, every SQL statement, every migration and every test, in your repository, under your version control. This page says what is there and why, what the library keeps and why, and what to reach for before editing a flow by hand. The line between the two is [ADR-0092](../adr/0092-what-the-framework-owns.md); the app's layout is [ADR-0083 §3](../adr/0083-modules-stack-migrations-and-ejection.md#3-app-layout).

There is no command that moves the line. That is the point of drawing it.

## The three layers

| Layer | Whose | What is in it |
|---|---|---|
| **Protocol and crypto** | The library's, permanently | [`gorbital.dev/modules/auth`](../methods/modules-auth.md): Argon2id parameters, session token generation and hashing, TOTP verification, API-key hashing, WebAuthn ceremonies, constant-time comparison, the authentication middleware and the permission catalog |
| **Flows** | Yours, written by `orb new` | `internal/modules/auth` and `internal/modules/orgs`: registration, verification, password reset, login, account linking, invitations, member management — their endpoints, their SQL and their migrations |
| **Policy** | Yours, and nowhere else | Role names, which role holds which permission, what a tenant is, who may join one, what happens on sign-up |

The first layer is where a small, invisible mistake is catastrophic and where a fix has to reach every application at once, so it is versioned and arrives with `go get`. The third is where any answer the framework picked would be wrong for the next product, so the framework never picks one ([tenancy](tenancy.md), [access control](access-control.md)). The middle layer is the interesting one: it is code you should be able to read and change, but it is also code nobody wants to write from scratch, so `orb new` writes it for you and then stops owning it.

## What `orb new` wrote

```text
internal/modules/auth/          package authhttp — the root package: New, Module, the options
├── authhttp.go, module.go
├── app_test.go, auth_test.go, …   its HTTP tests, which run against your copy
├── domain/                        the rules, with no database and no HTTP
├── usecase/                       one file per operation
├── repository/                    one SQL statement per file
│   └── migrations/                the embedded migrations
└── delivery/                      handlers, and the jobs the module contributes
    └── jobs/…
```

`internal/modules/orgs` is the same shape, holding `package orgshttp`. Four things about the copy are worth knowing, because they are what make it behave like the rest of your application rather than like a vendored dependency:

| | |
|---|---|
| **The package keeps its library name.** `internal/modules/auth` is `package authhttp`, imported as `authhttp "example.com/shop/internal/modules/auth"` | Every call in `main.go` reads exactly as the reference documentation writes it, so [`authhttp.New`](../methods/gorbital-authhttp.md)'s options and hooks are the ones below, and an example from the docs compiles against your copy |
| **The layers are flat.** The library's `internal/domain` is your `domain` | `internal/modules` is already internal to your app, and the generated architecture test allows the four layers under a module — the same rule that governs a module `orb gen module` writes |
| **The migrations are in `db/migrations`**, under the versions the library declared them with, and the module still declares them in `Module.Migrations` | A database migrated before the copy existed has nothing to apply: the merge treats a declared migration and a file with the same version and identical content as one migration ([ADR-0083 §6](../adr/0083-modules-stack-migrations-and-ejection.md#6-migrations)) |
| **`api/surface.json` records the module's error codes and audit actions** as your app's own names | `TestPublicSurface` compares them, so adding an error code to your copy is a change you see in a diff ([ADR-0054](../adr/0054-api-freeze-and-scaffold-compatibility.md)) |

A handful of the library's own test files are not copied. They are marked `//orb:noeject`, a source directive meaning *not copied into apps*: they test the library against the gorbital repository — the frozen v0.1.0 contracts, the golden apps' migrations — which your application does not have. Everything else is there, and `go test ./...` runs it.

`gorbital.lock` records where each copy came from: the module, the library package, the `gorbital.dev/gorbital` version, the date and a SHA-256 of the source as it was read. Nothing reads that entry at build time; it is what lets `orb doctor` tell you the library has moved on.

## What stays in the library

| Package | Why it is not yours |
|---|---|
| [`gorbital.dev/modules/auth`](../methods/modules-auth.md) | Layer 1. Password hashing, session tokens, TOTP, passkey and OAuth verification, API-key hashing. Your copy of the flows calls into it, and a fix here reaches you with `go get` |
| [`gorbital.dev/modules/orgs`](../methods/modules-orgs.md) | The organisation authorisation rules the flows call |
| [`gorbital.dev/gorbital/opshttp`](../methods/gorbital-opshttp.md) | The operations API under `/ops/`. Plumbing, not a business rule |
| [`gorbital.dev/gorbital/flagshttp`](../methods/gorbital-flagshttp.md) | `GET /v1/flags` |
| [`gorbital.dev/gorbital/mailevents`](../methods/gorbital-mailevents.md) | `POST /v1/webhooks/resend` |

The last three were copyable on demand before v0.2.2 and were never copied by default. They contain no business rules — an operations API, a flags endpoint, a webhook receiver — so nothing in the rule above asks for them to be yours, and they now stay in the library for good. If you need one of them to behave differently, the answer is an option, a guard or a module of your own beside it; if none of those reach it, open an issue, because a hook is cheaper for everyone than a copy.

## Before you edit a flow

The code is yours and you may change it. But a change you make in `internal/modules/auth` is a change you maintain, and several of the reasons people reach for one are already covered by something that survives every upgrade untouched:

| You want to | Reach for |
|---|---|
| Change password rules, require a second factor for a role, close registration, brand emails, add middleware to sign-in's routes | [Sign-in options](configuring-sign-in.md) |
| Refuse some sign-ins, record logins, create a profile or workspace with each account | [Sign-in hooks](sign-in-hooks.md) |
| Collect more fields at registration | [Extra registration fields](extra-registration-fields.md) |
| Sign in with a phone code, a magic link or another provider | [A sign-in method of your own](adding-a-sign-in-method.md) through `Authenticator.SignIn` |
| Name your tenant something other than an organisation, give it your own roles and ID format | [Tenancy](tenancy.md) — `orgshttp.ScopeName` changes the paths, the path parameter, the refusal code and the OpenAPI tag without touching a table |
| Decide which role holds which permission, or who may read a row | [Access control](access-control.md), [Resource access](resource-access.md) |
| Add routes beside a module's and protect them with its permissions | A module of your own ([Modules and routes](modules-and-routes.md)), with [`guard.Permission`](guards-and-middleware.md) or [`guard.Scope`](../start/organisations.md#in-an-app-on-gorbitalmain) |
| Limit `/ops/` to some addresses, choose the mail provider it reports | `OPS_ALLOWED_IPS`, [`opshttp.MailProvider`](../methods/gorbital-opshttp.md#MailProvider) |

An option or a hook is a line in `main.go` that the library keeps working. Editing a flow is a divergence you carry. Neither is wrong — the second is why the code is in your repository — but they cost differently, and the cheapest one that reaches the change is the right one.

Note also what is *not* on that list, because it is not a matter of cost: the `/v1/auth` path prefix, the error codes and their HTTP statuses, and the permission names are public API. You can change them in your copy, and your OpenAPI document and `api/surface.json` will say so; clients written against the contract will notice.

## Editing what you own

It is ordinary application code, so the ordinary rules apply, with two that catch people out:

**Add a migration; never edit one that has run.** `20260916000001_orgs.sql` has been applied to every database your app has — yours, CI's, production's. goose records it and never runs it again, so a column added to that file appears in new databases and silently never reaches the existing ones. Write the next migration instead.

**Keep the two copies of a migration identical.** The module declares its migrations and `db/migrations` holds them; `migrate` refuses the same version with different content and names both files. Change the module's tables with new migrations in `db/migrations`, as for any module.

The rest is unremarkable. Your copy may import another module's root package — the `orgs` module imports `authhttp` from `internal/modules/auth` — but not another module's layers, which the generated architecture test enforces. `orb gen modules` does not list these modules in `modules.gen.go`, because `main.go` adds them itself, in the position the library's went.

## When the library moves on

A fix to layer 1 reaches you with `go get`. A fix to a flow does not, because the flow is yours — so `orb doctor` tells you.

```text
ok       ejected   internal/modules/auth is the app's code, copied from
                   gorbital.dev/gorbital/authhttp v0.2.1 on 2026-09-18; the library's copy
                   hasn't changed since
```

The check compares the SHA-256 in `gorbital.lock` with the library package at the version `go.mod` requires. It warns when they differ, and quotes the changelog entries that name the package:

```text
warning  ejected   internal/modules/auth is the app's code, copied from
                   gorbital.dev/gorbital/authhttp v0.2.1 on 2026-09-18; the library's
                   authhttp has changed since (the app requires v0.2.2).
                   Changelog: …
                   → compare internal/modules/auth with authhttp in
                     gorbital.dev/gorbital v0.2.2 and port the fixes you need
```

Acting on it is yours to do: read the entries, print the library's directory with `go list -m -json gorbital.dev/gorbital`, diff it against `internal/modules/auth`, and take the fixes you need. `orb upgrade` never changes these modules' files and keeps their `gorbital.lock` entries.

This is the one thing a copy has that a fork does not: it knows where it came from, so the question *has anything happened upstream that I should know about?* has an answer.

## Apps that came from somewhere else

**An app created before v0.2.1** has sign-in and organisations in the library rather than in `internal/modules`. Nothing is wrong with it and nothing is deprecated; it keeps building. `orb upgrade --layout v0.2` writes the copies for a v0.1 app as part of the move ([Upgrading apps](../start/upgrading.md#move-to-the-v02-layout)).

**An app created without a tenancy** gains one with `orb add orgs`, which writes `internal/modules/orgs` the same way, on a branch ([Add organisations to an existing app](../start/upgrading.md#add-organisations-to-an-existing-app)).

**Going back to the library's module** is by hand, and possible only while your copy still matches it. Change the imports of `<app module>/internal/modules/<module>` back to `gorbital.dev/gorbital/<package>` and drop the name they were given (`auth` first, then `orgs`, since `orgshttp.Module` takes the authenticator); delete the directory and its entry in `gorbital.lock`; keep the migrations in `db/migrations`, where identical copies of the library's are harmless; then `go mod tidy`, `go build ./...`, `go test ./...`, and compare `api/openapi.json` with `go run ./cmd/api openapi --dir api`. Whatever you changed in the copy is lost, so move what you still need into options, hooks or modules of your own first.
