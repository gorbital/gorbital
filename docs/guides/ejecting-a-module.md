# Ejecting a module

How an app on `gorbital.Main` takes a built-in module into its own code with `orb eject`, when no option or hook covers a change: what the command writes, what the app gives up, and how to go back. The decision is [ADR-0083 §7](../adr/0083-modules-stack-migrations-and-ejection.md#7-ejection), with the [Phase 9 notes](../adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-orb-eject-2026-09-17); the command's flags and JSON output are in the [CLI reference](cli.md#orb-eject).

> [!NOTE]
> **Since v0.2.1, `orb new --preset full` ejects `auth` for you**, and `orgs` too with `--tenancy multi` — a new app already owns both. This page is for the other built-in modules (`flags`, `mailevents`, `ops`), for apps created before v0.2.1, and for apps created with `--no-eject`. What moves is the module's flows; the primitives they call — password hashing, session tokens, TOTP, passkey and OAuth verification in `gorbital.dev/modules/auth`, organisation authorisation in `gorbital.dev/modules/orgs` — stay in the library and are still fixed by `go get`.

| Module | Library package | What it is |
|---|---|---|
| `auth` | [`gorbital.dev/gorbital/authhttp`](../methods/gorbital-authhttp.md) | Sign-in: accounts, sessions, second factors, passkeys, API keys, `/ops/auth/users` |
| `flags` | [`gorbital.dev/gorbital/flagshttp`](../methods/gorbital-flagshttp.md) | `GET /v1/flags` |
| `mailevents` | [`gorbital.dev/gorbital/mailevents`](../methods/gorbital-mailevents.md) | `POST /v1/webhooks/resend` |
| `ops` | [`gorbital.dev/gorbital/opshttp`](../methods/gorbital-opshttp.md) | The operations API under `/ops/` |
| `orgs` | [`gorbital.dev/gorbital/orgshttp`](../methods/gorbital-orgshttp.md) | Organisations, members, invitations |

## Eject last

An ejected module is the app's code: fixes and features the library ships for it no longer reach the app, security fixes included. Try these first:

| You want to | Without ejecting |
|---|---|
| Change password rules, require a second factor for a role, close registration, brand emails, add middleware to sign-in's routes | [Sign-in options](configuring-sign-in.md) |
| Refuse some sign-ins, record logins, create a profile or workspace with each account | [Sign-in hooks](sign-in-hooks.md) |
| Collect more fields at registration | [Extra registration fields](extra-registration-fields.md) |
| Sign in with a phone code, a magic link or another provider | [A sign-in method of your own](adding-a-sign-in-method.md) through `Authenticator.SignIn` |
| Limit `/ops/` to some addresses, choose the mail provider it reports | `OPS_ALLOWED_IPS`, [`opshttp.MailProvider`](../methods/gorbital-opshttp.md#MailProvider) |
| Add routes next to a module's, protect them with its permissions | A module of your own ([Modules and routes](modules-and-routes.md)), with [`guard.Permission`](guards-and-middleware.md) or [`guard.OrgMember`](../start/organisations.md#in-an-app-on-gorbitalmain) |
| Something a hook would cover but none exists | An issue: hooks are cheaper for everyone than copies |

Eject when the change is to what the module itself does and none of those reach it: a different session model, another table layout, removing operations the API must not have.

## What orb eject does

```bash
orb eject orgs --dry-run --diff   # read the plan first
orb eject orgs
go build ./... && go test ./...
go run ./cmd/api openapi --dir api   # the document doesn't change
git add -A && git commit -m 'Eject orgs'
```

It is one plan, shown by `--dry-run` and applied at once ([ADR-0021](../adr/0021-generator-operation-model.md)), in a clean git tree (or with `--allow-dirty`), so the ejection is one commit to review.

### 1. The module's code

The package is copied at the version the app builds with: the `gorbital.dev/gorbital` version `go.mod` requires, from the module cache, or the directory a `replace` directive names. It lands in `internal/modules/<module>`, laid out like every module of the app ([ADR-0083 §3](../adr/0083-modules-stack-migrations-and-ejection.md#3-app-layout)):

```text
gorbital/orgshttp/                          internal/modules/orgs/
├── orgshttp.go, module.go                  ├── orgshttp.go, module.go          the root package: Module(auth, opts...)
├── app_test.go, orgs_test.go, …            ├── app_test.go, orgs_test.go, …    its HTTP tests
└── internal/                               ├── domain/
    ├── domain/                             ├── usecase/                        one file per operation
    ├── usecase/                            ├── repository/                     one SQL statement per file
    ├── repository/                         │   └── migrations/                 the embedded migrations
    │   └── migrations/                     └── delivery/
    └── delivery/                               └── jobs/orgspurge/             the job
        └── jobs/orgspurge/
```

| What changes in the copy | Why |
|---|---|
| `internal/<layer>/…` becomes `<layer>/…` | The app's architecture test allows the four layers under a module, and `internal/modules` is already internal to the app |
| Imports of the package and its layers name `<app module>/internal/modules/<module>/…`; so do imports of modules ejected before (an ejected `orgs` imports the ejected `auth`) | The copy uses its own code |
| The package keeps its name: `package orgshttp` in `internal/modules/orgs`, imported as `orgshttp "example.com/shop/internal/modules/orgs"` | No call in the app changes |
| Test files starting with `//orb:noeject <reason>` aren't copied | They test the library against the gorbital repository (the frozen v0.1.0 contracts, the golden apps' migrations), which an app doesn't have. The command lists them |

Nothing else changes: every other file is byte for byte the library's. The built-in modules import only gorbital's public API ([`operation.Register`](../methods/gorbital-operation.md), [`gorbital.AuthenticateAfterInput`](../methods/gorbital.md#AuthenticateAfterInput), [`gorbital.RetentionJob`](../methods/gorbital.md#RetentionJob)), which the library checks on every change, so the copy builds in the app.

### 2. The app's imports

Every Go file of the app that imports the library package imports the copy instead: `cmd/api/main.go`, modules that take the authenticator (Shelfie's `phonelogin` and `profiles`), tests, and modules ejected before. Calls stay as they are:

```diff
 import (
 	"gorbital.dev/gorbital"
-	"gorbital.dev/gorbital/orgshttp"
+	orgshttp "example.com/shop/internal/modules/orgs"
 )

 	gorbital.WithModules(orgshttp.Module(auth)), // unchanged: same options, hooks and arguments
```

`orb eject` refuses a `main.go` it can't follow this way: the package imported as `_` or `.`, or never called through its constructor (`authhttp.New`, `<package>.Module`).

### 3. Migrations

The module's migrations are copied into `db/migrations` under the versions the library declares them with, named as `gorbital.Migrate` names them (`20260916000001_orgs.sql`). The module still declares them in `Module.Migrations`; the merge treats a declared migration and a file with the same version and identical content as one migration ([ADR-0083 §6](../adr/0083-modules-stack-migrations-and-ejection.md#6-migrations)), so a database migrated before the ejection applies nothing. Keep both copies identical: `migrate` refuses the same version with different content, naming both files. Change the module's tables with new migrations in `db/migrations`, as for any module.

### 4. gorbital.lock and go.mod

`gorbital.lock` records the ejection (it is created when the app has none):

```json
"ejected": [
  {
    "module": "orgs",
    "package": "gorbital.dev/gorbital/orgshttp",
    "version": "v0.2.0",
    "date": "2026-09-17",
    "sha256": "8484331e…"
  }
]
```

`sha256` hashes the package's source as it was copied. Then `go mod tidy` runs (not with `--skip-tidy`), because the copied tests import packages the app didn't, and `api/surface.json` is recorded again: the module's error codes and audit actions are the app's own names now, and its `TestPublicSurface` compares them ([ADR-0054](../adr/0054-api-freeze-and-scaffold-compatibility.md)). Review the added names in the commit.

## Afterwards

| Tool | With an ejected module |
|---|---|
| `orb gen modules`, `orb dev` | Don't list it in `modules.gen.go`: `main.go` adds it where it added the library's, so modules keep their order |
| `orb doctor` | One `ejected` check per module: ok while the library's package, at the version `go.mod` requires, is the one copied; a warning when it changed, with the changelog entries that name the package; a failure when the directory is gone |
| `orb upgrade` | Keeps the entries in `gorbital.lock` and never changes the module's files; merging library changes into it is yours to do |
| Architecture test | A module may import another module's root package, such as the ejected sign-in's `authhttp`, never its layers |
| `orb eject` again | Refused: the module is the app's |

When `orb doctor` warns, read the changelog entries it quotes, compare `internal/modules/<module>` with the package in the module cache (`go list -m -json gorbital.dev/gorbital` prints its directory), and port the fixes you need.

### Order: orgs before auth

`orgshttp.Module` takes `*authhttp.Authenticator`. In an app that uses organisations from the library, the ejected sign-in's authenticator is another type, so `orb eject auth` refuses and says to run `orb eject orgs` first. Ejected in that order, the copy of `orgs` then imports the copy of `auth`.

## Going back to the library

There is no command: the copy may have changed. By hand, in a clean tree:

1. Change the imports of `<app module>/internal/modules/<module>` back to `gorbital.dev/gorbital/<package>` and drop the name they got (for `auth`, eject order reversed: `auth` first, then `orgs`).
2. Delete `internal/modules/<module>` and its entry in `gorbital.lock` (the whole file, when nothing else is recorded).
3. Keep the migrations in `db/migrations`: identical copies of the library's are harmless. Migrations you added for your changes stay applied in existing databases; write new ones that undo them if the library's module doesn't expect them.
4. `go mod tidy`, `go build ./...`, `go test ./...`, and compare `api/openapi.json` with `go run ./cmd/api openapi --dir api`.

Whatever you changed in the copy is lost; move what you still need into options, hooks or modules of your own first.
