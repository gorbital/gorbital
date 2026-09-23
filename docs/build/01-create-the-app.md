# 1. Create the app

One command creates Plateful. Two of its flags are not optional in the way flags usually are, and one thing it does at the end surprises people, so this chapter is longer than the command deserves.

```bash
orb new plateful --preset full --scope organisation --module example.com/plateful
```

## 1. The command, flag by flag

**What we're doing.** Creating the project.

**Why.** `orb new` writes a working app — a `main.go`, a database, sign-in, migrations, tests and an exported OpenAPI document — so the first chapter that matters can be about your domain rather than about wiring.

**What the framework already gives us.** All of that. `orb` is the CLI; `gorbital` is a library your app imports. Nothing generated here is magic — it is code you own and can read.

**What we build ourselves.** Nothing yet.

**How.** Every value can be passed as a flag or answered interactively. Leave a flag out in a terminal and you are asked for it — except the Go module path, which is the app name unless you pass `--module`:

| Question | Flag | Default |
|---|---|---|
| App name | `<name>`, positional | required |
| Go module path (never asked) | `--module` | the app name |
| Preset | `--preset minimal\|full` | **`minimal`** |
| Sign-in (Full only) | `--auth none\|basic\|full` | `full` |
| Scope (Full only) | `--scope none\|single\|custom\|<name>` | `single` |
| Initialise git | `--no-git` | yes |

Other flags: `--local <path>`, `--skip-tidy`, `--start`, `--no-start`, `--json`, `--yes`, `--no-input`, `--plain`.

`--module example.com/plateful` sets the Go module path every import in the app starts with. Without it, the module path is just `plateful`, which works locally and is awkward the moment the code goes anywhere. Use the path you would push to.

`--scope organisation` is what makes a restaurant an organisation: members, roles, invitations, personal workspaces, and data under `/v1/orgs/{orgId}/…`. The scope is chosen now; `orb add orgs` can convert a single-tenant app later, but starting where you mean to end is cheaper. The word is yours — `--scope merchant` mounts the same organisations module under `/v1/merchants/{merchantId}/…`, refusing with `merchant_not_found` ([Tenancy](../guides/tenancy.md)) — and Plateful uses the supplied `organisation` so the paths in this guide match the library's own. `--tenancy single` and `--tenancy multi` are the older names of `--scope single` and `--scope organisation`, and still work.

## 2. Why `--preset full` is not a preference

**This is the one irreversible decision in this chapter.** The default preset is `minimal`, and a Minimal app is a different kind of app that cannot become a Full one.

**What the two presets are:**

| Preset | What you get | Needs |
|---|---|---|
| **Minimal** | An HTTP API with configuration, telemetry, health checks, security headers and interactive docs. No database | Go |
| **Full** | Everything in Minimal, plus PostgreSQL, runtime settings, background jobs, email, sign-in and platform roles, the audit log, release tracking and the `/ops/*` APIs — and two example modules to read and delete | Go and Docker |

That difference is visible and reversible-sounding. The one underneath it is neither.

**The layouts.** gorbital has two app layouts, and the preset chooses one:

| Layout | Shape |
|---|---|
| **v0.2** | `cmd/api/main.go` runs the app on `gorbital.Main`; the app's modules live in `internal/modules`, listed by `modules.gen.go`; migrations in `db/migrations` |
| **v0.1** | A composition root in `internal/app` wires everything by hand, with `cmd/migrate` and `cmd/seed` beside `cmd/api` |

`--preset full` writes the **v0.2** layout. `--preset minimal` writes the **v0.1** layout — it composes the core packages directly, because it has no database and `gorbital.New` requires one. That is a deliberate decision, recorded as roadmap decision D17 and re-confirmed in [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md).

**What that costs you.** In a Minimal app:

`orb gen module` refuses:

```text
this app uses the v0.1 layout (internal/app/modules.go), and orb gen module writes modules for
apps on gorbital.Main; use orb gen resource <Name> <field:type>..., which writes the same layers
and registers them in internal/app
```

Its suggested alternative, `orb gen resource`, then refuses too, because a Minimal app has no sign-in module:

```text
… has no internal/modules/auth: resources belong to signed-in users, so orb gen resource needs
the Full preset's auth module
```

`orb gen migration` refuses, because there is nowhere to put one:

```text
… has no db/migrations: migrations and resources are generated in apps created with the Full
preset
```

Each refusal names the way out, and for a Minimal app the way out is always the same one: convert it. That is the suggestion that doesn't apply. `orb upgrade --layout v0.2` converts a *Full* v0.1 app, and refuses a Minimal one outright:

```text
the minimal preset has one layout, and keeps it: only Full apps move to v0.2
(roadmap decision D17)
```

> **Don't do this:** run `orb new plateful` and fix the preset later. There is no "later". Every generator this guide uses is unavailable, and the documented conversion path explicitly excludes the preset you'd be converting from.
> **Do this instead:** pass `--preset full` — or answer the question when `orb new` asks it. If you truly want a small API with no database, Minimal is a good choice; just make it knowingly, and don't expect the rest of this guide to apply.

Two smaller consequences of choosing wrong: a scope requires Full (`the minimal preset has no sign-in and no tenants, so --auth and --scope don't apply to it`), and a Minimal app has no `seed` command, so there is no administrator to sign in as.

## 3. What it prints — and what it does next

`orb new` writes the files, runs `go mod tidy`, and initialises git:

```text
creating plateful in ./plateful
auth full · scope organisation · library gorbital.dev v0.3.2

✓ wrote 65 files
✓ ran go mod tidy
✓ initialised git

created plateful

  api docs     http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)
  main.go      cmd/api/main.go runs the app on gorbital.Main; your code goes in internal/modules
  modules      orb gen module <Name> <field:type>... adds a table and its API
  emails       http://127.0.0.1:3100/mail (the Dev Portal catches every email in development)
  admin        admin@example.com; orb dev prints its password, 2FA key and recovery codes once
  sign-in      AUTH_PROVIDERS.md lists what to set for passkeys, Google and Apple
  orgs         every account gets a personal workspace; data lives under /v1/orgs/{orgId}
  invitations  set orgs.invitation_url to your frontend's page before inviting people
  email        Resend outside development; orb add mail switches to SMTP
  port 5432    taken? set POSTGRES_PORT in .env and the same port in DATABASE_URL
  ...

  next: cd plateful
        orb dev
```

It initialises git but **does not commit**. Do that yourself, before anything else:

```bash
cd plateful
git add -A && git commit -m "Create plateful"
```

`orb gen` and `orb add` refuse to run on a dirty tree, so every generated change afterwards stays a diff you can read. Committing now is what makes that useful.

### It starts `orb dev` for you

> [!WARNING]
> In a terminal, `orb new` does not stop when it has written the files. It changes into the new directory and runs `orb dev`, which starts Docker, migrates, seeds and opens the Dev Portal in your browser — and then keeps running until you press Ctrl-C.

It says so first:

```text
starting orb dev in plateful (Ctrl-C stops it; --no-start skips this)
```

The rule in the code is: start if `--start` was passed, **or** if `--no-start` was not passed and the command was interactive. "Interactive" means stdin and stdout are both a terminal and none of `--yes`, `--no-input`, `--json` or the `CI` environment variable are set. So:

| Invocation | Starts `orb dev`? |
|---|---|
| `orb new plateful --preset full --scope organisation` in a terminal | **Yes** |
| … with `--no-start` | No |
| … with `--yes`, `--no-input` or `--json` | No |
| In CI, or with output piped to a file | No |
| … with `--start`, anywhere | Yes |

This is deliberate — the first run is meant to end in the browser — but it is a surprise if you expected a command that creates a directory and exits. Pass `--no-start` when you are scripting, when you want to read the files before anything touches Docker, or when you simply want your prompt back:

```bash
orb new plateful --preset full --scope organisation --module example.com/plateful --no-start
```

[Chapter 3](03-configuration-and-first-run.md) runs `orb dev` deliberately and explains every step it takes.

### Inside a gorbital checkout, it uses that checkout

If you run `orb new` anywhere **inside a clone of the gorbital repository**, the new app is built against that working copy instead of the published library. `orb` walks up from the current directory looking for a `go.mod` whose first line is `module gorbital.dev`, and if it finds one, writes `replace` directives into the new app's `go.mod` — 21 of them for a Full app, one per library module, each pointing at an absolute path on your machine.

It is not silent, but the disclosure is one dim line in the header you have probably stopped reading:

```text
auth full · scope organisation · library ../.. (found above this directory; --local to change)
```

and, in the interactive flow, one answered question:

```text
✓ gorbital checkout … /Users/you/code/gorbital
```

Compare that with the line a normal run prints:

```text
auth full · scope organisation · library gorbital.dev v0.3.2
```

**Why it matters.** The app now builds against your local edits, including uncommitted ones. It will not build on anyone else's machine, or in CI, until the `replace` block is removed. And the path is not recorded in `gorbital.lock` — the lock deliberately ignores it, because it only ever reaches `go.mod` — so `orb upgrade` will not clean it up for you.

> **Don't do this:** create your app in a scratch directory that happens to be inside a gorbital clone, then wonder why CI can't resolve the dependency.
> **Do this instead:** create apps outside any gorbital checkout. If you are working *on* gorbital and want an app wired to your checkout, that is exactly what this feature is for — pass `--local <path>` explicitly, so the choice is in your shell history rather than in your current directory.

## 4. The tree it wrote

```text
plateful/
├── cmd/api/
│   ├── main.go              the whole app: a list of options passed to gorbital.Main
│   ├── mail.go              one function: the email provider (orb add mail rewrites it)
│   ├── storage.go           one function: S3-compatible file storage
│   ├── main_test.go         the app builds and its routes are what the document says
│   └── app_test.go          the app end to end, on a database of its own
├── db/
│   ├── migrations/
│   │   ├── migrations.go    //go:embed *.sql — the binary carries your schema
│   │   └── <version>_projects.sql
│   └── row_level_security.sql
├── internal/modules/
│   ├── modules.gen.go       the list main.go reads; orb regenerates it
│   ├── architecture_test.go the layering rule, enforced on every go test
│   ├── surface_test.go      freezes the public names in api/surface.json
│   ├── ping/                a module with no table: a public route and a runtime setting
│   └── projects/            what orb gen module writes: a table, five routes, four layers
├── api/
│   ├── openapi.json         the exported OpenAPI document
│   ├── openapi.baseline.json  the previous document, for breaking-change checks
│   ├── surface.json         error codes, permissions and audit actions, frozen by a test
│   ├── postman_collection.json
│   └── llms.txt
├── compose.yaml             PostgreSQL for development (and Grafana, off by default)
├── .env.example             every variable the app reads, commented line by line
├── gorbital.yaml            how the app was created; the orb CLI reads it
├── gorbital.lock            what orb wrote, and its hashes; orb upgrade reads it
├── go.mod, go.sum
├── Dockerfile, .dockerignore
├── .gitignore               .env and .orb/ are never committed
└── README.md, ARCHITECTURE.md, AGENTS.md, AUTH_PROVIDERS.md
```

Sixty-five files. The ones to understand now:

**`cmd/api/main.go`** is the entire wiring of the app, and it is short enough to read in a minute. Plateful's finished version — after the modules of the next chapters exist — looks like this:

<!-- include examples/apps/plateful/cmd/api/main.go#main -->

`gorbital.Main` reads configuration from the environment, connects to PostgreSQL, builds the settings, flags, jobs, email and middleware, and serves the modules' routes until it gets a stop signal. `options()` is a function rather than a literal so the tests can build the same app from it. [Chapter 2](02-framework-and-your-app.md) takes this file apart properly; [Your main.go](../guides/main-go.md) is the reference.

`gorbital.Main` also answers commands. `go run ./cmd/api help` lists them — `migrate`, `seed`, `openapi`, `version`, and seven more that arrive with sign-in, including `grant-role` and `rotate-auth-keys`. A module adds its own commands when `main.go` adds the module.

**`internal/modules/`** is where all your code goes. Each subdirectory is one module declaring `func Module() gorbital.Module`; `modules.gen.go` lists them:

```go
// Code generated by orb gen modules. DO NOT EDIT.
```

`orb gen modules` rewrites it, `orb dev` rewrites it before every build, and so does `go generate ./internal/modules`. It is committed, so the app builds without `orb` installed.

**`internal/modules/architecture_test.go`** enforces the layering — the domain imports nothing of the app, use cases import only their domain, repository and delivery import only use cases and domain, and no module imports another module's layers. It runs on every `go test`. [Chapter 5](05-the-restaurants-module.md) works inside those rules.

**`db/migrations/`** holds your tables, embedded so the binary carries them. The library's own tables are *not* here: `migrate` serves them from the library and runs them in one history with yours, ordered by version. [Chapter 6](06-migrations-and-the-database.md) is all of this.

**`api/`** is the app's exported surface, committed and checked by tests. `surface_test.go` fails when an error code, permission or audit action changes, so a rename you didn't mean is caught in review rather than by a client.

**`.env.example`** is the configuration for development, commented line by line, and the template `orb dev` copies to `.env`. [Chapter 3](03-configuration-and-first-run.md).

**`gorbital.yaml`** records how the app was created. It is plain and short:

```yaml
# gorbital project file: how this app was created. The orb CLI reads it.
apiVersion: gorbital.dev/v1
name: plateful
module: example.com/plateful
preset: full
tenancy: multi
# The app's layout: v0.2 apps run on gorbital.Main (cmd/api/main.go) with
# their modules in internal/modules (ADR-0083).
layout: v0.2
features: [postgres, settings, jobs, audit, mail, auth, orgs, ops]
# Email provider, changed with `orb add mail`: resend or smtp.
mail: resend
```

`orb dev` reads `features` to decide whether the app has a database. `orb gen` reads `tenancy` and `rls`. `orb doctor` reads the lot. Its sibling `gorbital.lock` records the `orb` release that created the app and a SHA-256 of every file `orb` wrote, so `orb upgrade` can rebuild those files as they were and merge newer templates into your edits. Commit both; edit neither by hand.

**Two example modules, to read and then delete.** `ping` is a module without a table — a public endpoint whose reply comes from a runtime setting. `projects` is exactly what `orb gen module` writes: a table, five routes, and the four layers [chapter 5](05-the-restaurants-module.md) reads one by one. Plateful deletes both once it has a migration of its own; keep at least one `.sql` file in `db/migrations` until then, because `//go:embed *.sql` fails the build on an empty directory.

## What just happened

You have an app that builds, tests and runs, with sign-in, organisations, an operations API and a database schema — and about sixty files you own outright. You also made the one decision in this guide you cannot revisit: `--preset full`, which is what `orb gen module` and every later chapter require.

Next: [chapter 2](02-framework-and-your-app.md) draws the line between the library and your code, then [chapter 3](03-configuration-and-first-run.md) starts the thing.
