Architecture review · Draft 1 · 14 Sep 2026

# APIStock: a Go application kit people can own

A Staff-level review of the plan for an open-source Go framework, CLI, module ecosystem and control plane. It covers what to build, what to hand off to existing projects, and what I would not build at all.

CLI: `aps` · Import path: `apistock.dev` · Target: Go 1.26+ · Status: for discussion, no code yet

## 01. Executive summary

The vision is good, but as written it is **four products**: a Go runtime library, a code generator with an upgrade story, a module ecosystem, and a hosted control plane with dashboard and mobile apps. Each of these is hard on its own. Start all four at once and you ship none of them well. Buffalo tried the batteries-included path in Go, and its main repository is now archived.

My recommendation is to build a **kit, not a framework**. That means two tightly scoped products for v1:

#### 1 · Runtime kit

Small, importable Go packages: lifecycle, config, HTTP helpers, health, observability setup, audit contract. Official modules for Postgres, auth, email, jobs. Plain constructors, no container, no reflection magic.

#### 2 · CLI + recipes

`aps` generates a real app and adds capabilities by writing thin, readable glue code into the developer's repo. It records what it wrote so a later `aps upgrade` can merge changes without clobbering edits.

#### Later, maybe · Control plane

A local dev console ships inside `aps dev` early. A hosted dashboard is a separate product built only on standard signals (OTLP, health endpoints, an admin API module). Mobile app: probably never.

The single most important design idea is **thin glue, thick library**. Anything that needs bug fixes and security patches over time lives in versioned Go packages that are upgraded with `go get`. The code we generate into the developer's repo is only the wiring and the parts they are meant to change. That split is what makes the upgrade problem solvable. Most scaffolding tools fail here.

The ten decisions that matter most:

1. Postgres only. `pgx` + `sqlc` + `goose`. No ORM, no database abstraction.
2. Standard library `net/http` routing. No custom router, no Fiber.
3. Manual constructor injection. No Fx, no Wire (archived Aug 2025), no custom container.
4. `log/slog` + OpenTelemetry, with zero-config local defaults.
5. Auth v1 = email/password, server-side sessions, verification, reset, simple RBAC. No JWT sessions.
6. Module migrations are copied into the app's single migration history, not run from inside libraries.
7. Modules are Go modules plus a declarative recipe. Distribution uses the Go module proxy, not our own package registry.
8. Community recipes cannot run arbitrary code at install time.
9. Multi-tenancy, dashboard, mobile and hosted GitHub App are all out of v1.
10. Build the reference app by hand first, then turn it into templates. Never write templates first.

## 02. Product vision, reframed

**Who it is for:** a Go developer or small team starting a production API or SaaS backend who wants the first two weeks of foundation work (auth, database, email, logging, jobs, audit, CI) done correctly in an afternoon, and who still wants to own and understand every line.

**The job:** "Give me the app a careful senior Go engineer would have set up, and keep it upgradeable."

**What it is not:** not a BaaS (PocketBase already does that well), not infrastructure-from-code (Encore's space), not a PaaS, not an identity server (Ory, Keycloak, Zitadel).

> **I would not recommend** positioning this as a "framework" to the Go community. Many Go developers are openly hostile to frameworks, and the word signals the magic you say you want to avoid. "Kit", "starter platform" or "toolkit" matches what you are actually describing, and it will get a fairer hearing.

### Where the real differentiation is

Every individual component already exists and is good: `net/http`, sqlc, River, OpenTelemetry, argon2. **Nobody wins by rewriting those.** The gaps in the Go ecosystem today are:

- **Integration:** the parts are not pre-wired with consistent context propagation, errors, audit, request IDs and tracing.
- **Upgradeability of starters:** Go boilerplates and templates are abandoned the day you clone them.
- **Security-correct defaults:** hand-rolled auth in Go apps is frequently wrong (token storage, reset flows, CSRF).
- **Local developer loop:** seeing traces, captured emails, jobs and audit events locally without running a Grafana stack.

If the project is excellent at those four things, it has a reason to exist. If it spreads into dashboards and mobile first, it doesn't.

## 03. Architectural principles

Your list of 16 is good but too long to use in a design review. When priorities conflict, a reviewer needs an ordering. Here it is compressed into seven, in priority order:

1. **Readable over clever.** A developer new to the project should be able to trace a request from `main.go` to the SQL using only "go to definition". No reflection-based wiring, no struct-tag routing, no runtime code loading.
2. **Owned code is sacred.** The CLI never silently overwrites a file the developer might have edited. Every write is previewed, recorded and reversible via git.
3. **Library for behaviour, generation for wiring.** Security-sensitive or bug-prone logic lives in importable packages. Generated code is glue and extension points.
4. **Standard first.** Prefer stdlib, then de-facto standards (OTel, pgx, sqlc), then our own code. Every custom component needs a written justification.
5. **Works with nothing running but Postgres.** No Redis, no Kafka, no collector, no SaaS required for a production deploy.
6. **Secure and observable by default, overridable by design.** Defaults are strong. Every default is a visible line of code the developer can change.
7. **Upgrade path designed before features.** No feature ships without a story for how apps generated on the previous version receive it.

A useful test for any proposal: *could a developer delete `aps` from their machine tomorrow and keep shipping this app?* If not, the proposal adds lock-in and needs a very strong reason.

## 04. Ecosystem boundaries

This is the most important section. Each component has a clear owner, a clear dependency direction, and a clear answer to "does the app need this at runtime?"

| Component | What it is | Needed at runtime? | Depends on | v1? |
|---|---|---|---|---|
| **Framework core** | Go module: lifecycle, config, httpx, health, obs setup, audit & actor contracts, testkit | Yes (imported) | stdlib, OTel API | **[Core]** |
| **Official modules** | Separate Go modules + recipes: postgres, auth, email, jobs, audit-pg | Yes, if added | core, their own libraries | **[Yes]** |
| **Community modules** | Same shape as official modules, in the author's own repo | Yes, if added | core (public API only) | **[Phase 6]** |
| **CLI** | Developer tool: create, add, gen, upgrade, dev, doctor | No | generator, recipe resolver | **[Yes]** |
| **Code generator** | Library inside the CLI: plan → render → check → apply → record | No | nothing at runtime | **[Yes]** |
| **Generated app** | The developer's repo. A normal Go module. | It *is* the runtime | core + chosen modules | **[Yes]** |
| **Local dev console** | Web UI served by `aps dev` on localhost; receives OTLP + captured mail | No (dev only) | standard signals only | **[Phase 5]** |
| **GitHub integration** | CLI step that delegates to `git`/`gh`; generated CI workflow files | No | git, optionally gh | **[Minimal]** |
| **Hosted control plane** | Separate product: fleet view of apps via OTLP, health, admin API | Never required | standard signals + admin module | **[Not v1]** |
| **Mobile app** | Client of the control plane API | Never | control plane | **[Probably never]** |

### Dependency rules

- Arrows only point downward: *app → modules → core → stdlib*. Core never imports a module. Modules never import each other directly, only through small interfaces declared in core or in the consuming module.
- Nothing in the runtime path imports the CLI, the generator, the console or the control plane.
- The control plane knows about apps only through **standard protocols** (OTLP, HTTP health endpoints, an opt-in admin API). It has no private back channel. That keeps it replaceable by Grafana, Datadog or nothing.

## 05. Proposed system architecture

```text
  DEVELOPER MACHINE                                        │  DEVELOPER'S INFRA (any host)
                                                           │
   ┌──────────────┐   reads recipes via     ┌──────────────┐   │
   │   aps CLI    │── Go module proxy ─────▶│ recipe index │   │     ┌───────────────────────────┐
   │ create · add │   (proxy.golang.org     │ (static JSON │   │     │   Generated app binary    │
   │ gen · upgrade│    + sum.golang.org)    │  in a repo)  │   │     │ ┌───────────────────────┐ │
   │ dev · doctor │                         └──────────────┘   │     │ │ your code (owned)     │ │
   └──────┬───────┘                                           │     │ ├───────────────────────┤ │
          │ plan → render → check → apply → record            │     │ │ glue (generated once) │ │
          ▼                                                   │     │ ├───────────────────────┤ │
   ┌──────────────────────┐   git / gh (optional)   ┌────────┐│     │ │ modules (imported)    │ │
   │  App repository      │────────────────────────▶│ GitHub ││     │ ├───────────────────────┤ │
   │  go.mod · app code   │                         │ (yours)││     │ │ core (imported)       │ │
   │  apistock.yaml/.lock │                         └────────┘│     │ └───────────────────────┘ │
   └──────┬───────────────┘                                   │     └──┬─────────┬──────────┬───┘
          │ aps dev                                           │        │ SQL     │ OTLP     │ /livez /readyz
          ▼                                                   │        ▼         ▼          ▼
   ┌──────────────────────┐                                   │   ┌────────┐ ┌─────────┐ ┌──────────────┐
   │ app (hot reload)     │── OTLP, SMTP ──▶ dev console      │   │Postgres│ │any OTel │ │load balancer │
   │ + compose: postgres  │                  localhost:9400   │   └────────┘ │backend  │ │/ orchestrator│
   └──────────────────────┘                                   │              └────┬────┘ └──────────────┘
                                                              │                   │ (optional, later)
                                                              │                   ▼
                                                              │           hosted control plane ◀── mobile
```

Figure 1: Everything left of the line is tooling. Everything right of it is a normal Go service with no runtime dependency on APIStock tooling.

How the parts talk to each other:

- **CLI ↔ recipes:** the CLI downloads module source through the standard Go module proxy and verifies it against the checksum database. The recipe lives inside the module. There is no custom package server.
- **CLI ↔ repo:** file writes only, always inside the project root, recorded in `apistock.lock`.
- **App ↔ modules:** ordinary Go function calls. Modules are constructed in `internal/app/app.go`.
- **App ↔ outside world:** Postgres, SMTP/API email provider, OTLP exporter, health endpoints. Everything else is optional.

## 06. Core runtime architecture

Core should be small enough that one person can read all of it in an afternoon. Target: under ~6k lines of non-test Go. If core grows past that, something that should be a module has moved in.

| Package | Responsibility | Built on |
|---|---|---|
| `apistock.dev/app` | Ordered start/stop of components, signal handling, graceful shutdown with deadline | `signal.NotifyContext`, `errgroup` |
| `apistock.dev/config` | Load struct from defaults → file → env; validate; `Secret` type that redacts itself in logs | `caarlos0/env`, `slog.LogValuer` |
| `apistock.dev/httpx` | Server with sane timeouts, middleware chain, `func(w, r) error` handler adapter, problem+json errors, request decoding + validation | `net/http` ServeMux (method + path patterns) |
| `apistock.dev/health` | `/livez`, `/readyz`; modules register named checks with timeouts | stdlib |
| `apistock.dev/obs` | One call sets up slog handler, tracer and meter providers; HTTP middleware for request ID, trace, access log, metrics | `log/slog`, OpenTelemetry SDK, `otelhttp` |
| `apistock.dev/actor` | "Who is doing this?" in `context.Context`. Knows nothing about auth. | stdlib |
| `apistock.dev/audit` | Audit `Event` type and `Recorder` interface; slog recorder for dev | stdlib |
| `apistock.dev/errs` | Small set of error kinds (NotFound, Invalid, Conflict, Unauthorized, Forbidden) mapped to HTTP status once, in httpx | `errors.Is/As` |
| `apistock.dev/testkit` | HTTP test helpers, Postgres test DB per test (template database clone), clock and ID fakes | `httptest`, testcontainers optional |

### Lifecycle contract

No "module registry" at runtime. A component that needs lifecycle implements one or both of two tiny interfaces. `app.Run` starts them in the order you list them and stops them in reverse.

```
// Shape only, not an implementation.
type Starter interface { Start(ctx context.Context) error }
type Stopper interface { Stop(ctx context.Context) error }

app.Run(ctx, logger, db, jobs, httpServer)   // explicit order, visible in main
```

This covers 95% of what Fx's lifecycle hooks do, with no container and no reflection. The dependency graph is just the order of lines in `app.go`, which is exactly what a developer reads first.

### HTTP

Since Go 1.22, `http.ServeMux` supports method matching and path wildcards (`GET /products/{id}`). That removes the main reason to pull in a router. Go 1.25 added `http.CrossOriginProtection` for CSRF defence. Chi remains a fine choice and is 100% `net/http`-compatible, so a developer can swap it in without any APIStock code caring.

> **I would not recommend** Fiber (or anything built on fasthttp) for this project. It is not `net/http`-compatible, so every middleware, OTel instrumentation and test helper in the ecosystem needs a special version. That cost multiplies across a module ecosystem.

## 07. Module architecture

A module has two halves that are versioned together but used at different times:

```text
  apistock.dev/modules/auth   (one Go module, one version tag)
  ├── *.go                 RUNTIME half: normal importable package(s)
  │                        imported by the app, upgraded with `go get`
  └── recipe/              TOOLING half: read by the CLI, never compiled into the app
      ├── apistock-module.yaml    manifest: identity, compatibility, needs/provides, config, operations
      ├── templates/       glue files written into the app once (owned by developer)
      ├── migrations/      SQL copied into the app's migration history
      └── docs.md          shown by `aps add auth --explain`
```

### Runtime contract

Each module exposes a plain constructor. Dependencies are explicit parameters, often small interfaces:

```
// Shape only.
func New(cfg Config, deps Deps) (*Module, error)

type Deps struct {
    DB     *pgxpool.Pool
    Mailer email.Sender     // interface: any email module satisfies it
    Audit  audit.Recorder   // interface from core
    Logger *slog.Logger
}
// Optional capabilities, discovered by type assertion in generated glue or called explicitly:
func (m *Module) Routes(mux *http.ServeMux, mw httpx.Chain)
func (m *Module) HealthChecks() []health.Check
func (m *Module) Start(ctx context.Context) error
```

### Dependencies between modules: capabilities, not names

The manifest says `needs: [postgres, email.sender]` and `provides: [email.sender]`. `auth` needs *an* email sender. `email-smtp`, `email-resend` and a community `email-ses` all provide one. The CLI resolves capabilities and asks when there are several choices. At runtime this is just a Go interface, so nothing is resolved dynamically.

### Lifecycle of a module in a project

| Stage | Command | What happens |
|---|---|---|
| Discover | `aps search email` | Query the static index (official + community, marked verified/unverified) |
| Inspect | `aps add auth --explain` | Show docs, needs/provides, files to create, files to edit, migrations, env vars, Go deps |
| Install | `aps add auth` | Resolve → plan → diff preview → confirm → apply → `go get` → record in `apistock.yaml`/`apistock.lock` |
| Configure | edit `.env` / `config.go` | Plain code and env vars; `aps doctor` validates |
| Upgrade | `aps upgrade auth` | Bump Go dep, copy new migrations, 3-way merge glue changes, run codemods (§21) |
| Remove | `aps remove auth` | Only removes files still unmodified since generation; lists the rest for manual cleanup. Never drops tables. |

### What belongs where: challenging the module list

| Capability | Verdict | Reasoning |
|---|---|---|
| Lifecycle, config, HTTP server, errors, health, logging/tracing setup | **[Core]** | Every app needs them; they are the contracts modules plug into. |
| Dependency injection | **[Core, as a convention]** | Not a package. Constructors + one visible `app.go`. |
| Routing | **[Stdlib]** | `net/http` ServeMux. Don't wrap it. |
| Audit contract | **[Core]** | Interface + event type only. Storage is a module. |
| Postgres, migrations, transactions | **[Official]** | Almost always used, but a pure webhook relay app doesn't need it. |
| Auth (users, passwords, sessions, RBAC) | **[Official]** | High value, security-critical, so it must be library-heavy. |
| Email (interface + SMTP + one API provider) | **[Official]** | Auth needs it. Keep providers thin. |
| Background jobs | **[Official, wraps River]** | Transactional enqueue in Postgres. Don't build a queue. |
| Audit Postgres store | **[Official]** | Append-only table, same-transaction writes. |
| Rate limiting | **[Official, small]** | In-memory by default; auth depends on it for login throttling. |
| OpenAPI docs | **[Official, later]** | Generate from handler types or spec-first; decide after v1 feedback. |
| OAuth/OIDC login, MFA, passkeys | **[Auth v1.x–v2]** | See §12. |
| Organizations / tenancy | **[Official, later]** | See ADR-013. Not core. |
| Storage (S3-compatible) | **[Official, later]** | Small interface over the AWS SDK; works with R2, MinIO. |
| Webhooks (outgoing) | **[Official, later]** | Needs signing, retries via jobs and an SSRF-safe dialer. Good second-wave module. |
| Cache | **[Community or later]** | Most early apps don't need Redis. Premature caching adds infra. |
| Events | **[Not a module]** | In-process: call functions. Durable: outbox via River in the same tx. A generic event-bus abstraction invites microservice cosplay. |
| Notifications (multi-channel) | **[Not v1]** | It's product logic (preferences, digests, channels). Email + jobs covers early needs. |
| Search, feature flags | **[Community / delegate]** | Use Postgres full-text first; OpenFeature Go SDK for flags. |
| Queues (generic, Kafka/NATS) | **[Community]** | Different problem from background jobs; don't abstract over brokers. |
| Admin UI | **[Generated, later]** | Admin screens are app code the developer must own and secure, not a dashboard feature. |
| GraphQL, payments, Twilio | **[Community]** | Opinionated, vendor-specific. Exactly what the ecosystem is for. |

## 08. CLI architecture

```text
 cmd/aps  (Cobra commands: thin, parse flags, print, exit codes)
   │
   ├── project     locate root, read apistock.yaml / apistock.lock / go.mod, check git clean state
   ├── recipe      fetch module@version via Go proxy, parse + validate apistock-module.yaml,
   │               resolve needs/provides, check compatibility ranges
   ├── gen         plan → render → check → preview → apply → record   (see §9)
   ├── upgrade     version diff, 3-way merge, migration copy, codemod runner
   ├── dev         process supervisor: compose deps, rebuild on change, dev console
   ├── doctor      config validation, version drift, missing migrations, vuln scan hint
   └── integrate   git init, gh repo create, CI template (all optional)
```

### Command surface (v1)

```
aps new <name> [--module path] [--with auth,email] [--github] [--yes]
aps add <module>[@version] [--explain] [--dry-run]
aps remove <module>
aps gen resource <Name> [field:type ...]
aps gen migration <name>
aps gen sql                 # runs the project's pinned sqlc (go tool sqlc generate)
aps upgrade [module|core] [--to vX.Y.Z]
aps dev
aps doctor
aps search <term>
```

> **I would not recommend** separate `create handler`, `create service` and `create repository` commands. They push projects toward layered, Java-style folders (`handlers/ services/ repositories/`) where one feature is spread across three directories. Idiomatic Go organises by feature. `aps gen resource` creates one package with the handler, service and store together, and developers split it only when it grows.

### CLI design rules

- **Every mutating command supports `--dry-run`** and prints a unified diff. It refuses to run on a dirty git tree unless `--allow-dirty` is passed, so `git checkout .` is always the undo button.
- **`--json` output and stable exit codes** on every command. This matters for CI and for AI coding agents, which will be a large share of the users driving the CLI.
- **Non-interactive by default in CI** (detects no TTY); prompts only when interactive.
- **No telemetry unless the user opts in**, and a public list of what is sent when they do.
- **Tool versions are pinned per project** using the `tool` directive in `go.mod` (Go 1.24+). Projects run `go tool sqlc` and `go tool goose`, not whatever is installed globally.
- **The CLI is optional after generation.** `go run ./cmd/api`, `go test ./...` and plain migration commands must all work without `aps`.

Cobra vs Kong: both are fine. I'd pick **Cobra**. It has shell completion, man pages and doc generation built in, and contributors already know it. Kong's struct-based API is nicer, but here the CLI surface is small and familiarity matters more.

## 09. Code-generation architecture

The template mess you want to avoid comes from one root cause: **templates that both create and later modify the same files, with no record of what was written**. The fix is an explicit ownership model plus a small set of typed operations.

### Three kinds of code

| Class | Examples | Who edits | How it is updated |
|---|---|---|---|
| **Library** (not in repo) | password hashing, session store, River wrapper, OTel setup | APIStock maintainers | `go get`. Semver. Codemods for breaking changes. |
| **Derived** (in repo, regenerated) | sqlc output, `*_gen.go` files; header `// Code generated … DO NOT EDIT.` | Nobody by hand | Regenerated deterministically from sources the developer owns (SQL files). |
| **Scaffold** (in repo, owned) | `main.go`, `app.go`, resource handlers, email templates, config structs | The developer | Written once. Later changes are offered as a 3-way merge on a git branch, never forced. |

The Go convention for generated files (`^// Code generated .* DO NOT EDIT\.$`) is already respected by gopls, linters and GitHub diffs. Use it instead of inventing markers.

### Pipeline

```text
 recipe@version + project state
          │
   1 RESOLVE   needs/provides, version ranges, conflicts with installed modules
          │
   2 PLAN      list of typed Operations (no file I/O yet)
          │        CreateFile · InsertAtAnchor · EnsureImport · AddGoRequire · AddTool
          │        CopyMigration · AppendEnvExample · AddComposeService · RunDerive(sqlc)
   3 RENDER    text/template with restricted funcs → go/format → go/parser validity check
          │
   4 CHECK     each op checks pre-conditions: file exists? hash matches apistock.lock? anchor found?
          │        → op is Applied / AlreadyDone (idempotent) / Conflict
   5 PREVIEW   unified diff; conflicts listed with the exact snippet to paste manually
          │
   6 APPLY     write via os.Root (confined to project dir), temp file + rename
          │
   7 RECORD    apistock.lock: recipe version, per-file class, base content hash
```

### Editing files the developer owns

Adding a module needs to change `app.go`: construct the module and mount its routes. Three ways to do that:

1. **Regex/string templates.** Break on the first edit. Rejected.
2. **Regenerate the whole file.** Destroys customisation. Rejected for owned files.
3. **AST edit at a named, visible anchor.** **[Chosen]**

Generated `app.go` contains a few comment anchors, for example `//aps:anchor modules` and `//aps:anchor routes`. `InsertAtAnchor` parses the file with a comment-preserving AST library (`dave/dst`), inserts the statement after the anchor, and runs `goimports`. If the developer moved or deleted the anchor, the operation **does not guess**. It reports a conflict and prints the three lines to paste. Anchors are ordinary comments: greppable, documented, and removable.

### Idempotency

- Every operation is written as "ensure state", not "do action". `EnsureImport` is a no-op if the import exists. `InsertAtAnchor` checks whether an equivalent statement is already there.
- Running `aps add auth` twice produces zero diff the second time. This is a CI test for every recipe.

### Schemas: SQL first, no DSL

> **I would not recommend** a custom schema language (YAML entity files, a Prisma-like DSL) as the source of truth. It becomes a second language to learn, it drifts from the real database, and every advanced Postgres feature needs special support. The source of truth is SQL: migrations define tables, `queries/*.sql` define access, sqlc derives Go types. `aps gen resource Product name:text price_cents:bigint` is just a shortcut that writes the first migration, CRUD queries and a handler scaffold, then gets out of the way.

### Why not full AST generation for everything?

Generating whole files through AST builders produces code that is hard to read in the generator itself. Contributors can't see what the output looks like. Use **templates for creating files** (readable, reviewable, the output is visible in the template) and **AST only for surgical edits** to existing files. Both are validated by `go/parser` and formatted before writing.

## 10. Generated application architecture

The generated app must look like something a thoughtful Go engineer wrote by hand. Organised by feature, with one obvious place where everything is wired together.

```
my-api/
├── cmd/api/main.go            owned    ~20 lines: load config, app.New, app.Run
├── internal/
│   ├── app/
│   │   ├── app.go             owned    THE wiring file: constructs modules in order, mounts routes (anchors)
│   │   ├── config.go          owned    one Config struct embedding each module's config
│   │   └── routes.go          owned    your own routes, middleware chain
│   ├── product/               owned    one package per feature (from aps gen resource)
│   │   ├── handler.go                  HTTP decode/encode, calls service
│   │   ├── service.go                  business rules, audit calls, transactions
│   │   ├── store.go                    thin adapter over sqlc queries
│   │   └── product_test.go
│   ├── db/
│   │   └── sqlc/              derived  sqlc output, DO NOT EDIT
│   └── emails/                owned    email templates copied from the auth recipe, restyle freely
├── db/
│   ├── migrations/            owned    single ordered history: yours + copied module migrations
│   └── queries/               owned    SQL used by sqlc
├── compose.yaml               owned    postgres, mailpit (dev only)
├── .env.example               owned    every env var, with comments
├── sqlc.yaml                  owned
├── apistock.yaml                    owned    installed modules + versions (human-readable intent)
├── apistock.lock                    tool     base hashes of scaffolded files (commit it)
├── ARCHITECTURE.md            owned    how this app is wired; written for humans and AI agents
├── .github/workflows/ci.yml   owned    vet, test, govulncheck (only if --github or --ci)
└── go.mod                     owned    includes `tool` lines for sqlc, goose
```

What is deliberately missing: no `pkg/`, no `handlers/ services/ repositories/` split, no `aps` runtime directory, no hidden `.APIStock/` folder of generated magic. Details in §23 and the workflow in §27.

## 11. Database strategy

### PostgreSQL only: yes, and say so loudly

Supporting several databases makes every module author test against several databases, forces lowest-common-denominator SQL, and blocks the Postgres features that make a Postgres-only stack simple: transactional job queues (River), `LISTEN/NOTIFY`, row-level security, `jsonb`, advisory locks, full-text search. "One database, used well" is a feature.

> **I would not recommend** a database abstraction layer. You would pay its cost in every module forever, for users who mostly never switch. If a MySQL community fork appears later, that's a sign of success, not a v1 requirement.

### Data access: pgx + sqlc

| Option | Architectural consequence | Verdict |
|---|---|---|
| **sqlc** | SQL is the source of truth; derived Go code is plain and readable; no runtime layer; queries reviewable by DBAs. Weak on dynamic filters (use a small hand-written query or `squirrel` there). | **[Default]** |
| pgx directly | Maximum control; lots of scanning boilerplate. Always available as an escape hatch next to sqlc. | **[Escape hatch]** |
| Ent | Schema-as-Go-code plus a large derived API. Powerful graph traversals, but a big generated surface, its own migration story, and modules shipping Ent schemas would force Ent on every app. | **[No]** |
| GORM | Reflection, implicit queries, surprising behaviour (zero values, auto-preload, hooks). The opposite of "minimal magic". | **[No]** |
| Bun / sqlx | Reasonable middle ground, but a second convention next to sqlc brings no clear gain. | **[Allowed in apps]** |

Modules must not force any of this on application code. A module's store is internal to that module and uses its own sqlc output compiled into the module. Apps can use anything for their own tables.

### Migrations: goose, copied into the app

Should modules own their migrations? **Modules author them; the app owns them once installed.**

| Option | Pros | Cons |
|---|---|---|
| A. Modules embed and run their own migrations at startup (per-module version table) | Zero steps for users | Ordering across modules is undefined; hidden schema changes on deploy; can't review or squash; FK from your table to `auth_users` may run before it exists |
| B. **Copy module migrations into `db/migrations` at add/upgrade time** | One linear, reviewable history; works with any migration tool; developer controls deploy timing; git shows every schema change | Upgrades need a CLI step (`aps upgrade`) to bring new migrations |
| C. Declarative schema diffing (Atlas) | Excellent tooling | Key features have moved behind a commercial tier (e.g. `migrate lint` left the free plan in v0.38). Risky as a hard dependency for an OSS foundation. |

**Decision: B**, using goose (SQL files, `embed.FS`, Provider API, and Go migrations when a data backfill needs code). Files are timestamp-named, so a copied module migration sorts to "now" in the app history, which is the correct order: it was added after everything already there.

- **Ordering:** timestamp names across all sources; the CLI refuses to copy a module migration whose declared `after:` dependency (e.g. `postgres/0001_extensions`) is not present.
- **Upgrades:** module v1.3 → v1.4 may ship new migrations only. Never edit a released migration. `apistock.lock` records which module migrations were copied, so `aps upgrade` copies only new ones.
- **Naming:** module tables use a prefix (`auth_users`, `audit_events`, `river_*`) to avoid collisions without schema-per-module complexity.
- **Running:** `aps migrate up` is a convenience wrapper around `go tool goose`. Apps may also run migrations as a separate deploy step or at startup (flag, off by default in production).

### Transactions

One helper, used everywhere, so audit events and job enqueues can commit atomically with the business change:

```
// Shape only.
err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
    p, err := queries.WithTx(tx).CreateProduct(ctx, args)
    if err != nil { return err }
    if err := auditStore.RecordTx(ctx, tx, audit.Event{Action: "product.created", ...}); err != nil { return err }
    _, err = jobs.InsertTx(ctx, tx, IndexProductArgs{ID: p.ID}, nil)   // River: runs only if tx commits
    return err
})
```

### Seed data

Seeds are Go programs in `cmd/seed`, not SQL dumps, so they use the same services and validation as the app. Module recipes may contribute a seed function (e.g. create a first admin) that is called from the generated seed `main`.

## 12. Authentication strategy

Auth is where "generate the code and let them own it" is **dangerous**. If password hashing and token handling are copied into every app, a security fix can't reach anyone. So the auth module is library-heavy: the security core is imported; only HTTP handlers, email templates and UI-facing responses are scaffolded.

### v1 scope

| Feature | Version | Design |
|---|---|---|
| Users, email + password | **[v1]** | argon2id (`x/crypto/argon2`), parameters stored with the hash so they can be raised later with rehash-on-login. Case-insensitive unique email (`citext` or lowered column). |
| Sessions | **[v1]** | Opaque 32-byte random token. Only its SHA-256 is stored in `auth_sessions`. Cookie `__Host-session` (Secure, HttpOnly, SameSite=Lax) for browsers; same token as `Authorization: Bearer` for API clients. Idle + absolute expiry, rotation on privilege change, revoke-all. |
| Email verification | **[v1]** | Single-use hashed token, 24h TTL, sent via jobs. |
| Password reset | **[v1]** | Single-use hashed token, 30–60 min TTL, same response whether or not the email exists, all sessions revoked on success. |
| Login throttling | **[v1]** | Per-account and per-IP limits; no hard lockout (lockout is itself a DoS vector). |
| Roles & permissions | **[v1]** | Permission strings (`product:write`), roles are named sets, user↔role table. `authz.Require("product:write")` middleware. `Authorizer` interface so apps can plug in OpenFGA, Cedar or Casbin. |
| Security events | **[v1]** | Emitted to `audit.Recorder`: login succeeded/failed, password changed, reset requested, session revoked. |
| OAuth/OIDC social login | **[v1.1]** | `golang.org/x/oauth2` + `coreos/go-oidc`; PKCE, state + nonce; account linking only on verified email. |
| TOTP MFA + recovery codes | **[v1.2]** | Encrypted TOTP secret (app-level key), hashed recovery codes. |
| Passkeys (WebAuthn) | **[v2]** | `go-webauthn/webauthn`. |
| Organizations / tenants | **[Separate module]** | See ADR-013. |
| SAML, SCIM, enterprise SSO | **[Delegate]** | Point users to Ory, Keycloak, Zitadel or WorkOS. Provide an "external identity" adapter: trust an OIDC ID token, map to local user. |

> **I would not recommend** JWTs as the session mechanism. You can't revoke them without a server-side denylist, which rebuilds sessions with extra steps. Refresh-token rotation adds a whole class of bugs. Opaque server-side sessions in Postgres are simpler, revocable and plenty fast. JWTs remain appropriate for service-to-service calls and for consuming third-party OIDC tokens. Refresh tokens become relevant only when a mobile client is in scope.

### Boundaries

- Auth middleware resolves a session and puts an `actor.Actor` into the context. **Nothing else in the system imports the auth module** to learn who is acting. Audit, jobs and your services read `actor.From(ctx)`.
- The `User` type is deliberately minimal (id, email, verified, created). Profile fields belong in the app's own table keyed by user id. This keeps the module upgradeable.

## 13. Observability strategy

Goal: good telemetry with **zero decisions on day one** and **no lock-in on day 300**.

```text
  handler ─ service ─ store
     │ ctx carries: request_id · trace/span · actor
     ▼
  slog.Logger ──── handler: text (dev) | JSON (prod) ── stdout      ◀ always
     │            + trace_id/span_id/request_id attrs added from ctx
     └─(opt)──── otelslog bridge ──┐
  OTel tracer ─────────────────────┤
  OTel meter ──────────────────────┼── OTLP exporter  (only if OTEL_EXPORTER_OTLP_ENDPOINT set)
                                   │         └──▶ aps dev console | Grafana | Honeycomb | Datadog | …
                                   └── Prometheus /metrics (opt-in exporter)
```

### Defaults

- **Logs:** `log/slog`. Colourised text in dev, JSON in production, always to stdout. Zap and zerolog are not needed; slog is the stdlib contract every module can accept as `*slog.Logger`.
- **Correlation:** a middleware reads or creates `X-Request-ID`; a slog handler wrapper injects `request_id`, `trace_id` and `span_id` from context into every log line. Developers get correlated logs even with tracing export turned off.
- **Tracing and metrics:** OpenTelemetry SDK. HTTP server via `otelhttp`, pgx via the otelpgx tracer, River via its OTel middleware. Exporters are configured through the standard `OTEL_*` env vars, not APIStock-specific settings.
- **Logs via OTLP:** offered as opt-in. The OTel Go logs API/SDK reached release-candidate status in 2026 and isn't yet under v1 stability guarantees. Stdout JSON stays the default until it is.
- **Health:** `/livez` (process is up, no dependency checks, so a DB blip doesn't restart every pod) and `/readyz` (registered checks: DB ping, migrations applied, River client healthy).
- **Error reporting:** a tiny `ErrorReporter` hook called by the recover middleware and job failure handler. Sentry/Honeybadger are community modules; the default reporter logs.

### What developers must understand on day one

Nothing. They pass `ctx` (which Go already requires) and use `logger.InfoContext(ctx, ...)`. When they're ready for tracing they set one env var or run `aps dev`, and the console shows the traces.

## 14. Audit architecture

Audit logs answer "who did what to which thing, when, from where, and did it work", for compliance and security investigations. They are **not** application logs (different retention, integrity and access rules) and **not** analytics.

### Event model

| Field | Type | Notes |
|---|---|---|
| `id` | UUIDv7 | Time-ordered; good index locality |
| `occurred_at` | timestamptz | Set by the recorder, not the caller |
| `actor_type`, `actor_id`, `actor_label` | text | `user` \| `service` \| `system` \| `anonymous`. Label is snapshotted (email may change later). |
| `action` | text | Dotted past tense, namespaced by module: `auth.session.revoked`, `product.price.changed` |
| `resource_type`, `resource_id` | text | Nullable for actions without a target (e.g. failed login) |
| `outcome` | text | `success` \| `failure` \| `denied`. Denied authorization attempts are audit-worthy. |
| `request_id`, `trace_id` | text | Joins the audit trail to logs and traces |
| `ip`, `user_agent` | inet, text | Only when the event came from a request; PII, so it's subject to retention policy |
| `tenant_id` | text, nullable | Reserved now so an orgs module can fill it without a migration of existing rows |
| `metadata` | jsonb | Structured detail; before/after for changed fields. Recorder applies a redaction list. |

### Decoupling

- `apistock.dev/audit` in core has only the `Event` type, a `Recorder` interface, and a helper that fills actor/request fields from `ctx`. It imports `apistock.dev/actor`, never auth.
- Any module or app code records events. Auth is just one producer.
- Storage is pluggable: `audit/pgstore` (official, append-only table, `RecordTx` for same-transaction writes), slog recorder (dev), and later OTLP-logs or S3/WORM sinks.

### Integrity

- v1: the app's DB role gets `INSERT, SELECT` only on `audit_events` (the migration grants this). No update/delete path in the store API.
- Later: optional hash chaining per partition, monthly partitioning with retention jobs, export to object storage with object lock.

> Write audit events **in the same transaction** as the change whenever possible. Async audit writes lose events on crash, which defeats the purpose. Failed and denied attempts are written outside the (rolled-back) business transaction.

## 15. Configuration architecture

### Model

Configuration is **a Go struct**. Each module defines its own `Config` with defaults and validation; the app's `config.go` embeds them. A developer sees every setting with "go to definition".

```
// Shape only.
type Config struct {
    Env      string        `env:"APP_ENV" envDefault:"development"`
    HTTP     httpx.Config  `envPrefix:"HTTP_"`
    DB       postgres.Config `envPrefix:"DATABASE_"`
    Auth     auth.Config   `envPrefix:"AUTH_"`
    Email    smtp.Config   `envPrefix:"SMTP_"`
}
```

### Precedence (lowest to highest)

1. **Code defaults** in each module's config struct
2. **`.env` file**, loaded in development only (never required in production)
3. **Environment variables**: the production source of truth, 12-factor
4. **`*_FILE` variants** for secrets (`DATABASE_PASSWORD_FILE=/run/secrets/db`), which covers Docker/Kubernetes secrets and systemd credentials without a secrets SDK

> **I would not recommend** YAML/TOML config files with per-environment overlays (`config.dev.yaml`, `config.prod.yaml`) in v1. They create a second precedence system next to env vars, and "which file won?" becomes a support burden. Environments differ by env vars. Nothing else.

### Rules

- **Validate at startup and fail fast** with one error listing every invalid or missing value. `aps doctor` runs the same validation.
- **Secrets use a `config.Secret` type** whose `LogValue()` and `String()` return `[redacted]`, so printing the config struct is safe.
- **No runtime-mutable config in v1.** Things that change at runtime (feature flags, per-tenant settings) are data, stored in the database by the module that owns them, not "config".
- **Dashboard-managed config:** not recommended. See §16.
- **Secret managers** (Vault, AWS/GCP Secret Manager) are community modules that resolve values before `config.Load`, or are handled by the platform injecting env vars.

## 16. Dashboard architecture

Of your five options, the answer is **none of them as stated**. Split the idea into three different things that are currently merged under one name:

| What people mean by "dashboard" | Where it belongs | When |
|---|---|---|
| See my routes, requests, traces, logs, captured emails, jobs and audit events **while developing** | **Local dev console** served by `aps dev` on localhost. Receives OTLP and SMTP locally; reads the dev DB. No accounts, no cloud. | **[Phase 5]** |
| Manage users, roles, reset a password, view audit trail **in production** | **Admin module generated into the app**. It's app functionality protected by the app's own auth and permissions, deployed with the app. | **[Post-v1 module]** |
| Create apps, see health/metrics/deploys **across many apps and environments** | **Hosted control plane**: a separate product and repo that consumes standard signals. Optional forever. | **[Only with demand]** |

### Why not "part of the framework"

- A web UI in the runtime adds attack surface, frontend dependencies and release coupling to every app, including apps that never open it.
- A dashboard that manages production configuration or users from outside the app becomes a **privileged back door into every customer app**. Your security story then depends on securing a SaaS, which is exactly the proprietary dependency you want to avoid.

### If you build the hosted control plane later

- It is **read-mostly** and consumes only OTLP, `/readyz` and the admin module's authenticated API. Grafana could replace it with no app changes.
- It never sits in the request path and never holds app database credentials.
- "Create application" in the UI runs the same generator as the CLI, and the output is a repo in the user's GitHub. The UI is a skin over the CLI, not a second code path.
- It is also the natural **business model** (hosted telemetry and team features) if you need one, with the open-source kit remaining complete without it.

> **I would not recommend** starting the hosted dashboard before the kit has real users. Today it would be a UI for projects that don't exist, and it would absorb most of a small team's capacity. Existing tools (Grafana with the `otel-lgtm` image, Sentry, a hosting provider's dashboard) cover production monitoring well already.

## 17. Mobile architecture

> **I would not recommend** building a mobile app, and I'd remove it from the roadmap rather than leave it as Phase 9. The functions listed (health, alerts, logs, deploy status, notifications) are already served by PagerDuty/Opsgenie, Grafana and Sentry mobile apps, GitHub Mobile, and hosting providers. Reading logs on a phone is a poor experience. Nobody picks a Go backend kit because it has a phone app.

If a control plane exists and paying users ask for mobile, the architecture is simple and needs no design now:

- The mobile app is **only a client of the control plane's public API**. It never talks to user apps directly and holds no app credentials.
- Scope: incident alerts via push, health overview, acknowledge/mute. Read-only for everything else.
- A responsive control-plane web UI plus push notifications through an existing alerting integration covers 90% of this for 5% of the effort.

## 18. GitHub integration architecture

The design principle: **GitHub integration is a convenience performed by the developer's own tools, on their own account, producing plain files.** After it runs, there is no connection between the repo and APIStock.

```text
 aps new my-api --github
   1  generate files locally                       (always)
   2  git init, first commit "aps new (APIStock vX.Y.Z)"  (always; uses local git + user's identity)
   3  write .github/workflows/ci.yml               (only with --github or --ci)
   4  create remote repo + push:
        if `gh` is installed and authenticated → run `gh repo create my-api --private --source . --push`
        else → print the exact commands to run; do nothing further
```

### Decisions

- **Delegate authentication to `gh`.** The aps CLI never asks for, stores or transmits a GitHub token in v1. That removes the most sensitive thing a code-generating CLI could hold.
- **Private by default**; the command prints what it will do and asks for confirmation (skipped with `--yes`).
- **CI is a plain workflow file**: `go vet`, `go test -race`, `govulncheck`, `golangci-lint`, migration check against a Postgres service container. Actions pinned by commit SHA. No APIStock-hosted actions required.
- **Portable:** `--ci gitlab` writes `.gitlab-ci.yml` instead. Nothing about the app depends on the forge.
- **Dependabot/Renovate config** is generated so aps module upgrades show up as normal PRs. `aps upgrade` can run in a scheduled workflow to open PRs for recipe-level changes (Phase 4+).

> **I would not recommend** a APIStock-hosted GitHub App or OAuth app in v1. It would require storing user tokens on your servers, make you a supply-chain target for every connected repo, and create exactly the platform dependency you want to avoid. Revisit only for the hosted control plane, with fine-grained, repo-scoped installation tokens.

## 19. Open-source / community module architecture

### Identity and distribution

- **A module is identified by its Go module path**: `github.com/acme/apistock-stripe`. Ownership is whoever controls that path. No new naming authority to run or dispute.
- **Distribution is the Go module proxy.** `aps add github.com/acme/apistock-stripe@v1.2.0` downloads the module zip through `GOPROXY` and verifies it against `sum.golang.org`. Private modules work via `GOPRIVATE` like any Go dependency. Published versions are immutable.
- **Short names** (`aps add stripe`) resolve through the **index**: a public git repo containing one small YAML file per module (path, description, tags, owner, verification level). Adding a module is a pull request. The website and `aps search` read a static JSON build of that repo. No database, no upload server.

### Trust levels

| Level | Requirements | CLI behaviour |
|---|---|---|
| **[Official]** | In the APIStock org, maintained by core team, in the compatibility test matrix | Installs after normal preview |
| **[Verified]** | Index PR reviewed; owner verified via repo; releases with GitHub artifact attestations / Sigstore signing; passes `aps module check` in CI; 2FA on org | Installs after preview; shows owner |
| **[Listed]** | Index PR merged, automated checks only | Extra warning + explicit confirmation |
| Unlisted | Any Go module path with a recipe | Requires full path and `--trust` flag |

### Versioning, dependencies, compatibility

- Semver via git tags, enforced by Go itself (`/v2` path for breaking majors).
- Manifest declares `APIStock: ">=1.3.0 <2.0.0"` (core API range) and capability `needs`. Go-level dependency resolution (MVS) stays Go's job. The CLI only checks capability and core-range compatibility.
- `aps module check` (run by authors in CI): manifest schema, templates render and compile against a fresh app, recipe idempotency (apply twice = no diff), migrations apply and roll forward on Postgres, `go vet`, `govulncheck`.

### Security model for recipes

Recipes are **declarative data**: a manifest, templates and SQL files, executed by the CLI's fixed set of operations. A third-party recipe cannot run shell commands, download extra files, or write outside the project root at install time. That capability list is what `--explain` shows, like a permission prompt.

The runtime Go code of a module has the same trust level as any `go get` dependency: it runs with full app privileges. The CLI can't sandbox that, and it shouldn't pretend to. The mitigations are visibility (diff preview, `go.sum`), `govulncheck`, trust levels and signed releases.

> If recipes ever need custom logic (e.g. complex codemods), the right escalation is a WASI-sandboxed generator with only project-directory access. That's a Phase 8+ decision, not v1.

A full worked example is in §28.

## 20. Security architecture

### Runtime (generated apps)

| Threat | Default control |
|---|---|
| Password compromise | argon2id with stored params; breach-list check hook (k-anonymity HIBP) opt-in; min length 12, no composition rules (NIST 800-63B) |
| Session theft | `__Host-` cookie, Secure/HttpOnly/SameSite=Lax; hashed tokens at rest; rotation on login & privilege change; idle + absolute expiry |
| CSRF | `http.CrossOriginProtection` (Fetch metadata / Origin checks) on cookie-authenticated routes; bearer-token routes are not CSRF-prone |
| CORS | Off by default; explicit origin allowlist in config; never `*` with credentials |
| Brute force / abuse | Rate-limit middleware (`x/time/rate`, keyed by IP + account); auth endpoints limited by default |
| SQL injection | sqlc parameterised queries; lint rule flags `fmt.Sprintf` into query strings |
| SSRF | Outbound HTTP for user-supplied URLs (webhooks, avatar fetch) goes through a safe client that blocks private/link-local/metadata IPs after DNS resolution |
| Info leakage | problem+json errors hide internals in prod; panics recovered and reported, never echoed; secrets redacted in logs |
| Slowloris / resource exhaustion | Server read-header, read, write, idle timeouts; `MaxBytesReader` on bodies |
| Security headers | Middleware: HSTS (prod), `X-Content-Type-Options`, `Referrer-Policy`, frame-ancestors; CSP left to apps that serve HTML |
| Authorization gaps | Deny-by-default helper for route groups; `denied` outcomes audited; resource generator scaffolds a permission check in the service, not only the handler |
| Vulnerable deps | `govulncheck` in generated CI; Dependabot/Renovate config generated |

### CLI and supply chain

| Threat | Control |
|---|---|
| Malicious recipe writes outside project (`../../.ssh`) | All writes through `os.Root`; template output paths validated; symlinks not followed out of root |
| Recipe runs arbitrary code at install | No exec operation exists; templates use `text/template` with a fixed, side-effect-free func map |
| Recipe injects malicious code into owned files | Mandatory diff preview; git-clean requirement; trust levels; anchors insert only statements parsed and printed from the template |
| Tampered module download | Go proxy + checksum DB; `apistock.lock` stores module sum |
| Compromised aps CLI release | Reproducible builds via GoReleaser; Sigstore signatures + SLSA provenance; `go install` path also available (sumdb-verified) |
| Maintainer account takeover | Required 2FA in org; protected tags; two-person release approval for official modules |
| GitHub token leakage | APIStock never handles tokens in v1 (delegates to `gh`); later: OS keychain only, fine-grained scopes, never in files or env dumps |
| Typosquatting (`aps add strpie`) | Short names only from the reviewed index; full paths shown in confirm prompt |
| Telemetry privacy | Off by default; documented schema; no paths, names or code sent |

Also: a `SECURITY.md` with a disclosure process from day one, a security advisory channel for modules, and a policy that auth, sessions and crypto code get two-maintainer review.

## 21. Versioning and upgrade strategy

This is the part most likely to decide whether the project survives. Scaffolding tools usually fail because v1 apps can't reach v2. The strategy has four independent mechanisms, one for each kind of change:

| Kind of change | Where it lives | Mechanism |
|---|---|---|
| Bug fix, security patch, new feature in behaviour | Library code | `go get` / Dependabot PR. No CLI needed. This is why §3 principle 3 exists. |
| Deprecated / renamed API | Library code | Keep old function as a wrapper with `//go:fix inline` for at least one minor version. Go 1.26's `go fix` rewrites callers automatically. |
| Structural API break in a major version | Library + owned code | APIStock ships `go/analysis` analyzers with suggested fixes (`aps upgrade` runs them), plus a written migration guide. |
| Improved scaffold (new middleware in `app.go`, new email template) | Owned code | 3-way merge using the base recorded in `apistock.lock` |
| New tables/columns | Migrations | New migration files copied in; released migrations never change |

### The `apistock.lock` baseline

For each scaffolded file, `apistock.lock` records the recipe version and the hash of the content APIStock originally wrote. The original content itself is reproducible from `recipe@version` via the module proxy, so the lock file stays small.

```text
 aps upgrade auth --to v1.6.0         (requires clean git tree; works on branch aps-upgrade/auth-v1.6.0)

 for each scaffolded file of auth:
   BASE  = render(auth@v1.4.0 recipe)       ← what we wrote originally
   OURS  = file on disk                     ← developer's current version
   THEIRS= render(auth@v1.6.0 recipe)       ← what we'd write today

   OURS == BASE            → replace with THEIRS          (untouched file, safe)
   BASE == THEIRS          → leave alone                  (template unchanged)
   otherwise               → git merge-file OURS BASE THEIRS
                              clean  → apply
                              conflict → write markers, list file in summary

 then: go get auth@v1.6.0 → copy new migrations → run analyzers/go fix
       → go build ./... && go test ./... → print summary → developer reviews & merges the branch
```

This is the model Copier and cruft use for project templates, and it's the same mental model as `git merge`, which developers already know. The key property: **the tool never destroys a change; the worst case is a merge conflict on a branch.**

### Version policy

- **Core**: semver; one major per 18–24 months at most; previous major gets security fixes for 12 months after the next major ships.
- **Official modules**: independent semver; each declares its supported core range. A published **compatibility matrix** is tested in CI (every official module × supported core minors × two latest Go versions).
- **Go version**: support the two most recent Go releases, matching Go's own support policy.
- **Deprecation**: nothing is removed in a minor. Everything deprecated has a `go fix` path or an analyzer.
- **Dogfooding rule**: before any release, the reference app generated with the previous release is upgraded with `aps upgrade` in CI. If that upgrade needs manual work, the release notes must say exactly what.

## 22. Repository strategy

**Recommendation: C, hybrid.** One monorepo for everything that must be tested together, separate repos for things with a different stack, cadence or ownership.

| Repository | Contents | Why separate / together |
|---|---|---|
| `aps` (monorepo, several Go modules) | core, official modules, CLI, recipes, reference app, docs site, compatibility tests | An API change in core, a template change and its effect on the reference app land in **one PR with one CI run**. This is the main thing that keeps recipes and libraries from drifting. |
| `apistock-index` | module index YAML files | Community PRs without write access to the main repo; different review rules |
| `apistock-console` (later) | hosted control plane | Different stack (TypeScript front end), possibly different licence, must not be required to build the kit |
| Community modules | author's own repos | Ownership = Go module path |

### Multiple Go modules inside the monorepo

Core and each official module get their own `go.mod`. An app using only core doesn't pull pgx into its module graph, and a River major bump doesn't force a core release. Tags are path-prefixed (`modules/auth/v1.4.0`), which Go supports natively. A `go.work` file ties them together for local development.

> Cost: releasing many modules needs tooling (e.g. a release script or release-please monorepo mode). Budget a day for it in Phase 1. Don't split core itself into several Go modules; that's where multi-module pain actually starts.

Why not a single Go module for everything: every user would get every dependency in `go.sum` (AWS SDK, Stripe, River), and any module's breaking change would force a core major version. Why not full multi-repo: cross-cutting changes need coordinated PRs across repos, and templates drift from the libraries they call. That's the classic failure mode of generator projects.

## 23. Go project structure (framework monorepo)

```
apistock/
├── go.work                     local dev across modules (not used by consumers)
├── core/                       go module apistock.dev
│   ├── app/                    lifecycle: Run, Starter, Stopper, shutdown
│   ├── config/                 Load, Validate, Secret
│   ├── httpx/                  server, middleware chain, handler adapter, problem+json
│   ├── health/                 checks registry, /livez /readyz handlers
│   ├── obs/                    slog handlers, OTel setup, HTTP middleware
│   ├── actor/                  actor in context
│   ├── audit/                  Event, Recorder, slog recorder
│   ├── errs/                   error kinds
│   └── testkit/                test helpers (imported only by tests)
├── modules/
│   ├── postgres/               go module apistock.dev/modules/postgres  (+ recipe/)
│   ├── auth/                   go module apistock.dev/modules/auth      (+ recipe/, internal/store with sqlc)
│   ├── email/                  interface + smtp/ + resend/ subpackages (+ recipe/)
│   ├── jobs/                   River integration (+ recipe/)
│   ├── auditpg/                Postgres audit store (+ recipe/)
│   └── ratelimit/
├── cli/                        go module apistock.dev/cli
│   ├── cmd/aps/                main package: `go install apistock.dev/cli/cmd/aps@latest`
│   └── internal/
│       ├── command/            cobra commands (thin)
│       ├── project/            apistock.yaml, apistock.lock, go.mod reading
│       ├── recipe/             manifest schema, fetch via proxy, resolve
│       ├── plan/               typed operations + check/apply
│       ├── render/             templates, gofmt, parse-validate
│       ├── astedit/            anchor insertion (dst)
│       ├── upgrade/            3-way merge, analyzers runner
│       ├── devserver/          watcher, process supervisor
│       ├── console/            local dev console (OTLP/SMTP receivers + embedded UI)
│       └── forge/              git, gh delegation, CI templates
├── recipes/app/                base "aps new" recipe (the reference app, templatised)
├── examples/reference-app/     hand-written app; generated app must match it (golden test)
├── internal/compat/            compatibility matrix test harness
├── docs/                       docs site source; ADRs in docs/adr/
└── .github/                    CI, release workflows
```

### Why these directories

- **`core/` as the root module path** gives the shortest import path (`apistock.dev/httpx`) for the most-used code.
- **No `pkg/`**: in a library repo everything non-internal is public by definition. `pkg/` adds path length and no meaning.
- **`recipe/` lives inside each module** so the template and the library it calls are always versioned in the same tag. This is the single most important structural decision against drift.
- **`cli/internal/`**: the generator is not a public API in v1. Exposing it too early freezes internals that will change a lot. Community extension happens through recipes, not Go APIs of the generator.
- **`examples/reference-app/`** is written by hand, reviewed as normal Go code, and a golden test asserts that `aps new` + `aps add` reproduces it. Templates follow the reference app, not the other way round.
- **`internal/compat/`** at repo root is tooling for maintainers, never imported by users.

## 24. Technology choices

| Area | Choice | Justification | Replaceable by app? |
|---|---|---|---|
| Language | Go 1.26+ (two latest releases) | `tool` directive, `os.Root`, new `go fix`, `CrossOriginProtection` | n/a |
| HTTP routing | `net/http` ServeMux | Method/wildcard patterns since 1.22; universal compatibility | Yes (chi, any net/http router) |
| CLI | Cobra | Completion, docs gen, contributor familiarity | n/a |
| Config | struct + `caarlos0/env` | Tiny, tag-based, no global state | Yes |
| Validation | `go-playground/validator` | De facto standard; used only at request/config edges | Yes |
| Postgres driver | `pgx` v5 + `pgxpool` | Fastest, most complete Postgres driver; native types | No (modules depend on it) |
| Queries | `sqlc` (pinned via `go tool`) | SQL as source of truth; readable output; actively maintained (1.31, Apr 2026) | Yes for app tables |
| Migrations | `goose` v3 | SQL + Go migrations, `embed.FS`, MIT, no commercial tier | Yes (files are plain SQL) |
| Background jobs | River | Postgres-native, transactional enqueue, generics, periodic jobs, UI available | Yes |
| Logging | `log/slog` | Stdlib contract; no third-party logger in public APIs | Handler yes |
| Traces/metrics | OpenTelemetry Go SDK | Vendor-neutral industry standard; stable for traces and metrics | Exporter yes |
| Password hashing | argon2id (`x/crypto`) | OWASP recommendation | No |
| OIDC | `x/oauth2` + `coreos/go-oidc` | Mature, minimal | n/a |
| AST editing | `dave/dst` + `x/tools/imports` | Comment-preserving edits; goimports behaviour | n/a |
| IDs | UUIDv7 (`google/uuid`) | Time-ordered, native Postgres `uuid` type | Yes |
| Dev email capture | Built-in SMTP sink in dev console (Mailpit in compose until then) | No external account needed for auth flows locally | Yes |
| Releases | GoReleaser + Sigstore/cosign + GitHub attestations | Signed, reproducible, SLSA provenance | n/a |

## 25. Ecosystem research & alternatives considered

### What existing projects teach

| Project | Got right | Got wrong / complaints | Lesson for APIStock |
|---|---|---|---|
| Go stdlib | ServeMux patterns, slog, `os.Root`, `go fix` inline | Leaves integration to you | Build on it; integrate, don't wrap |
| Chi | 100% net/http, tiny | Less needed since 1.22 | Compatibility beats features |
| Gin / Echo | Productive, huge adoption | Custom context type leaks into every handler; ecosystem split | Never introduce a custom `Context` |
| Fiber | Fast, Express-like | fasthttp incompatibility with net/http tooling | Don't fork the ecosystem for benchmarks |
| Buffalo | Rails-like end-to-end experience | Too much surface for maintainers; generated apps hard to upgrade; main repo archived | Scope is the killer. Thin glue, thick library. |
| Beego / Goravel | Batteries included | Port another language's framework idioms (Laravel facades, global app) into Go | Stay idiomatic; no service locator |
| PocketBase | Single binary BaaS, usable as a Go library, great DX | Opinionated data model (collections, SQLite); not a general app foundation | If users want a BaaS, recommend PocketBase instead of competing |
| Encore | Excellent local dev dashboard; infra from code; tracing out of the box | Annotations + static analysis compiler; hosted provisioning isn't open source; you write "Encore Go" | Copy the local dev console idea; reject the custom compiler |
| Wire | Compile-time DI, no reflection | Archived Aug 2025; extra generation step for something constructors do | Manual wiring in a generated, readable file |
| Fx | Lifecycle, modular graph | Reflection, runtime errors, stack traces hard to read, "where does this come from?" | Two tiny lifecycle interfaces instead |
| sqlc | SQL-first, readable output | Dynamic queries awkward | Adopt; document the escape hatch |
| Ent | Powerful typed graph API | Large generated surface; lock-in to its schema | Don't let modules force an ORM |
| GORM | Fast start | Magic, surprising queries, perf traps | Avoid |
| goose | Simple, SQL + Go, embed | Provider API still marked experimental | Adopt; wrap thinly so it can be swapped |
| Atlas | Declarative schemas, great linting | Key features moved to paid tier (lint in v0.38) | Don't hard-depend; allow as an app choice |
| OpenTelemetry | Vendor-neutral standard | Setup complexity; Go logs signal only at RC in 2026 | Hide setup behind one call + env vars; slog stays default for logs |
| Zap / Zerolog | Fast structured logging | Pre-slog; non-standard APIs in libraries | slog in all public APIs |
| Ory Kratos / Keycloak | Complete identity, SSO, compliance | Separate service to run; heavy for small apps; enterprise features licensed | Embedded auth for the 80%; adapter to these for enterprise |
| Temporal | Durable workflows | Cluster to operate; overkill for background jobs | River for jobs; Temporal as a community module |
| Asynq | Simple, Redis-based | Adds Redis; no transactional enqueue with your DB | River fits "Postgres only" |
| Rails / Laravel / Phoenix | Generators, conventions, first-party auth (`phx.gen.auth` generates owned auth code) | Upgrades between majors are painful; runtime magic | Copy `phx.gen.auth`'s ownership idea, keep security core in a library |
| shadcn/ui | Copy-source-in ownership model with a registry; users customise freely | No upgrade path for copied components | Same ownership model, plus `apistock.lock` 3-way merges to fix its upgrade gap |
| Copier / cruft | Template updates via recorded baseline + 3-way merge | Python-only, template-level not module-level | Proven model for §21 |
| .NET Aspire dashboard | OTLP-receiving local dev dashboard | Tied to .NET orchestration | Validates the dev-console-over-OTLP design |

### Competitive analysis

| Area | Existing approach | Problem | APIStock approach | Honest assessment |
|---|---|---|---|---|
| Auth | Roll your own; Kratos/Keycloak service; Auth0/Clerk SaaS | DIY is often insecure; services add ops; SaaS adds cost + lock-in | Embedded library with owned handlers; adapter to external IdPs | Real gap in Go. Worth building, but it's a long-term security commitment. |
| Database | GORM/Ent or hand-rolled pgx | Magic or boilerplate; no module migration story | pgx + sqlc + goose, copied module migrations | Components already excellent; our value is only the conventions. |
| Observability | Manual OTel setup per project | Hours of boilerplate; easy to get context propagation wrong | One setup call, env-var config, correlated slog, local console | Moderate value; console is the differentiator. |
| CLI | `gonew`, cookiecutter-style templates, Buffalo CLI | One-shot copy; no add/upgrade | Plan/apply generator with lock file and upgrades | Largest differentiator and largest risk. |
| Modules | `go get` + README instructions | Every integration is manual wiring | Go module + declarative recipe via Go proxy | Valuable only if the upgrade story works; otherwise it's a fancy README. |
| Code gen | sqlc, oapi-codegen, templates | Great for derived code; nothing for owned scaffolds | Three code classes + 3-way merge | Borrowed from proven tools; new to Go. |
| Dev loop | `air` + docker compose + Mailpit + Jaeger | Four tools to set up | `aps dev` with built-in console | Encore proves developers love this. |
| Whole backend | PocketBase, Supabase | Not a code-first Go app | Not competing | If the user wants a BaaS, they should use those. |

## 26. Architecture Decision Records

Status for all: **Proposed**. These should live in `docs/adr/` in the monorepo and be accepted or rejected before Phase 1 starts.

- [ADR-001: Framework boundaries](adr/0001-framework-boundaries.md)
- [ADR-002: Module architecture](adr/0002-module-architecture.md)
- [ADR-003: Code generation](adr/0003-code-generation.md)
- [ADR-004: Dependency injection](adr/0004-dependency-injection.md)
- [ADR-005: Database strategy](adr/0005-database-strategy.md)
- [ADR-006: Authentication](adr/0006-authentication.md)
- [ADR-007: Observability](adr/0007-observability.md)
- [ADR-008: Configuration](adr/0008-configuration.md)
- [ADR-009: Repository strategy](adr/0009-repository-strategy.md)
- [ADR-010: Dashboard architecture](adr/0010-dashboard-architecture.md)
- [ADR-011: GitHub integration](adr/0011-github-integration.md)
- [ADR-012: Versioning and upgrades](adr/0012-versioning-and-upgrades.md)
- [ADR-013: Multi-tenancy](adr/0013-multi-tenancy.md)

## 27. Example developer workflow

```
$ go install apistock.dev/cli/cmd/aps@latest
$ aps new my-api --module github.com/acme/my-api
  ✓ created 23 files · git init · first commit
  next: cd my-api && aps dev

$ cd my-api
$ aps add postgres
  plan  + compose.yaml service "postgres"
        + internal/app/app.go   (insert at //aps:anchor modules: db := postgres.MustOpen(...))
        + db/migrations/20260914101500_postgres_extensions.sql
        + go.mod require …/modules/postgres v1.2.0 · tool sqlc · tool goose
  apply? [y/N] y

$ aps add auth
  needs email.sender → choose provider: [smtp] resend  → smtp
  plan  + 9 files · 3 migrations · 6 env vars · edits app.go (2 anchors)
  apply? [y/N] y

$ aps gen resource Product name:text price_cents:bigint
  + db/migrations/…_create_products.sql   + db/queries/products.sql
  + internal/product/{handler,service,store,product_test}.go
  ~ internal/app/routes.go  (mount at //aps:anchor routes)
  ✓ ran sqlc generate

$ aps dev
  postgres ● mailpit ● migrations up (5) ● api :8080 ● console http://localhost:9400
```

Observability isn't a separate `aps add` step. The base app already has slog, request IDs, health checks and OTel wiring. That removes one of your proposed commands, on purpose.

### What was generated, and why

| File | Class | Why it exists |
|---|---|---|
| `cmd/api/main.go` | scaffold | Entry point; readable in 20 lines; `go run` works without APIStock |
| `internal/app/app.go` | scaffold | The one place dependencies are constructed and ordered |
| `internal/app/config.go` | scaffold | Every setting discoverable by go-to-definition |
| `internal/auth/handlers.go` | scaffold | Login/signup/reset HTTP shapes you'll want to change; calls the auth library |
| `internal/emails/*.html` | scaffold | Your brand, your copy |
| `internal/product/*` | scaffold | Feature package; starting point, not a pattern you must keep |
| `internal/db/sqlc/*` | derived | Regenerated from your SQL; never edited |
| `db/migrations/*` | owned history | One reviewable schema timeline |
| `apistock.yaml` / `apistock.lock` | tool | Intent and baselines for add/upgrade; safe to delete if you abandon APIStock |
| `ARCHITECTURE.md` | scaffold | Explains wiring, code classes and conventions to new teammates and AI agents |

A condensed `app.go` after those commands, to show the level of magic (none):

```
// Shape only.
func New(ctx context.Context, cfg Config) (*App, error) {
    tel, err := obs.Setup(ctx, cfg.Obs)                  // slog + OTel
    if err != nil { return nil, err }

    //aps:anchor modules
    db    := postgres.MustOpen(ctx, cfg.DB, tel)
    jobs  := jobs.New(db, tel)
    audit := auditpg.New(db)
    mail  := smtp.New(cfg.Email)
    users := auth.New(cfg.Auth, auth.Deps{DB: db, Mailer: mail, Audit: audit, Jobs: jobs, Logger: tel.Logger})
    products := product.NewService(product.NewStore(db), audit)

    mux := http.NewServeMux()
    //aps:anchor routes
    users.Routes(mux)
    product.Routes(mux, products, users.Require)

    srv := httpx.NewServer(cfg.HTTP, mux, tel, health.Handlers(db, jobs))
    return &App{parts: []any{tel, db, jobs, srv}}, nil  // app.Run starts in order, stops in reverse
}
```

## 28. Example community module: `apistock-stripe`

### Repository

```
github.com/acme/apistock-stripe/
├── go.mod                  module github.com/acme/apistock-stripe
├── stripe.go               New(cfg, deps), Client wrapper, Routes()
├── webhook.go              signature verification, idempotent event handling via jobs
├── internal/store/         sqlc-generated access to stripe_* tables
├── recipe/
│   ├── apistock-module.yaml
│   ├── templates/
│   │   └── internal/billing/handlers.go.tmpl   owned: checkout + portal endpoints
│   ├── migrations/
│   │   ├── 0001_stripe_customers.sql
│   │   └── 0002_stripe_webhook_events.sql
│   └── docs.md
├── stripe_test.go          unit tests with stripe-mock
├── recipe_test.go          apstest: apply recipe to a fresh app, build, test, re-apply = no diff
├── .github/workflows/      test, aps module check, govulncheck, release with attestations
└── README.md
```

### Manifest

```
apiVersion: apistock.dev/v1
name: stripe
module: github.com/acme/apistock-stripe
description: Stripe customers, checkout and verified webhooks
license: MIT
apistock: ">=1.2.0 <2.0.0"
needs: [postgres, jobs, audit.recorder]
provides: [billing.provider]

config:                                  # becomes a struct field + .env.example lines
  prefix: STRIPE_
  vars:
    - { name: SECRET_KEY, secret: true, required: true }
    - { name: WEBHOOK_SECRET, secret: true, required: true }

migrations:
  dir: migrations
  after: [postgres/extensions]

operations:                              # fixed vocabulary; no exec
  - ensureImport: { file: internal/app/app.go, path: github.com/acme/apistock-stripe }
  - insertAtAnchor:
      file: internal/app/app.go
      anchor: modules
      code: 'billing := stripe.New(cfg.Stripe, stripe.Deps{DB: db, Jobs: jobs, Audit: audit})'
  - insertAtAnchor: { file: internal/app/app.go, anchor: routes, code: 'billing.Routes(mux)' }
  - createFile: { template: templates/internal/billing/handlers.go.tmpl }
  - addConfigField: { file: internal/app/config.go, field: 'Stripe stripe.Config `envPrefix:"STRIPE_"`' }
```

### Lifecycle for the author

| Step | What they do |
|---|---|
| Scaffold | `aps module new github.com/acme/apistock-stripe` creates the layout above with a passing test suite |
| Develop | `aps module try ../my-api` applies the local recipe to a test app (with a local `replace`) |
| Test | `go test ./...` + `aps module check`: schema, render, compile, idempotency, migrations on real Postgres, upgrade from previous tag |
| Document | `recipe/docs.md` is shown by `aps add stripe --explain` and rendered on the index site |
| Version | `git tag v1.3.0`; breaking runtime API → `/v2`; new migrations only append |
| Publish | Push tag → proxy picks it up. First time: PR to `apistock-index` adding `modules/stripe.yaml`; automated checks run; reviewer grants Listed, then Verified |
| Install (user) | `aps add stripe` → index → proxy download + sumdb verify → preview → apply |
| Ownership | Whoever controls the repo. Name disputes follow index policy; transfers via index PR signed off by both parties. |

`apistock-redis` would be the same shape with no migrations, `provides: [cache.store, ratelimit.store]`, a `/readyz` check, and a compose service operation.

## 29. Phased roadmap

I changed your order in three ways. The reference app is written by hand *before* the generator. Upgrades are proven before the ecosystem opens. Mobile is removed. GitHub integration shrinks to part of Phase 3 because the delegate-to-`gh` design makes it small. Time estimates assume 2–3 strong engineers.

**P0 (3–4 wks)**

### Architecture & spikes

**Build:** Accept ADRs; spikes: dst anchor editing, 3-way merge via `git merge-file`, goose copy-in ordering, OTel one-call setup; choose the name (check Go module path, domain, trademark).

**Don't build:** Any public API.

**Why:** The upgrade/merge spike can invalidate ADR-003. Find out now.

**Done when:** ADRs accepted; spike reports written; a 3-way merge demo survives a hand-edited `app.go`.

**P1 (6–8 wks)**

### Core runtime + hand-written reference app

**Build:** core packages (§6), postgres module, reference app with a real feature (products) written by hand, testkit, release automation, docs skeleton.

**Don't build:** CLI, auth, dashboard, recipes.

**Why:** Templates extracted from a reviewed, working app are good. Templates written first become the "mess" you want to avoid.

**Done when:** Reference app runs with graceful shutdown, correlated logs, traces to an OTLP backend, `/readyz`, >80% core test coverage, core < 6k LOC.

**P2 (8–10 wks)**

### Auth, email, jobs, audit (as libraries)

**Build:** auth v1 scope (§12), email (smtp + one API provider), jobs (River), auditpg, ratelimit; integrated into the reference app; external security review of auth.

**Don't build:** OIDC, MFA, orgs, admin UI.

**Why:** These define the module contracts. Proving them by hand first keeps recipes thin later.

**Done when:** Reference app has signup → verify → login → reset → RBAC-protected product CRUD with audit events in the same transaction; security review findings closed.

**P3 (8–10 wks)**

### CLI + generator (+ minimal GitHub)

**Build:** `aps new`, `add`, `remove`, `gen resource`, `gen migration`, `doctor`; plan/apply engine; `apistock.lock`; recipes for official modules; `--github` via `gh`; CI template.

**Don't build:** Community recipe loading, index, `upgrade`, dev console.

**Why:** The generator only has to reproduce what already exists by hand.

**Done when:** Golden test: `aps new` + `aps add …` reproduces the reference app byte-for-byte (modulo names); re-running any command yields no diff; first public alpha.

**P4 (6 wks)**

### Upgrades

**Build:** `aps upgrade` (3-way merge on branch, migration copy, analyzers runner), compatibility matrix CI, deprecation tooling, first real minor release shipped as an upgrade.

**Don't build:** New modules.

**Why:** Before anyone depends on the ecosystem, prove that apps can move forward.

**Done when:** An app generated on v0.1 and edited by hand upgrades to v0.2 in CI with zero lost edits; public beta.

**P5 (6 wks)**

### `aps dev` + local console

**Build:** Watcher/reload, compose management, embedded OTLP + SMTP receivers, console UI: requests/traces, logs, emails, jobs, audit events, routes.

**Don't build:** Accounts, cloud sync, production views.

**Why:** High-delight, zero-lock-in feature; the strongest adoption driver after auth.

**Done when:** Password-reset email and its trace both visible in the console within 1s of the request; works offline.

**P6 (8 wks)**

### Module SDK, index, 1.0

**Build:** Manifest v1 freeze, `aps module new/try/check`, `apistock-index` repo + static site, trust levels, security policy, OIDC login (auth v1.1), S3 storage and webhooks modules as reference second-wave modules.

**Don't build:** Hosted marketplace, ratings, payments, sandboxed generators.

**Why:** Open the ecosystem only when contracts are stable enough to promise compatibility.

**Done when:** At least 3 external authors published modules using only public docs; core, CLI and official modules tagged 1.0.

**P7 (ongoing)**

### Depth: orgs, MFA, admin module, OpenAPI

**Build:** Driven by issue data: likely orgs module, TOTP, generated admin screens, OpenAPI generation.

**Don't build:** Anything without repeated user demand.

**Done when:** Each module passes the same upgrade and security gates as v1 modules.

**P8 (gated)**

### Hosted control plane (optional product)

**Build:** Only if the gate is met: meaningful production adoption and repeated requests for multi-app views. OTLP ingest, health overview, alerts, team access, "create app" as a UI over the generator.

**Don't build:** Config management of production apps, mobile app, anything required by the kit.

**Done when:** An app can connect by setting OTLP env vars only, and disconnect by removing them.

## 30. Major risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Scope creep: building dashboard/mobile/marketplace before the kit is good | High | Fatal | Phase gates in §29; "do not build" list; one-page scope doc per phase |
| Upgrade story doesn't work in practice; users stay on old versions and churn | Medium | Fatal | P0 spike; P4 before ecosystem; upgrade CI gate on every release |
| Security incident in auth module damages trust permanently | Medium | Severe | Library-heavy design, external review, two-maintainer rule, advisory process, bug bounty when funded |
| Maintainer burnout: N modules × M core versions × Go versions | High | Severe | Few official modules; community owns vendor integrations; matrix automation; clear deprecation windows |
| Go community rejects it as "a framework" | Medium | High | Position as kit; zero-runtime-dependency proof; idiomatic reference app people can read before trying |
| Malicious or abandoned community modules | Medium | High | Declarative recipes, trust levels, index review, deprecation flags in the index |
| Generated code quality drifts from idiomatic Go over time | Medium | Medium | Reference app golden test; lint generated output in CI |
| Upstream changes (River, goose Provider API, OTel logs) break assumptions | Medium | Medium | Thin wrappers at those seams; pin versions in recipes; compat matrix |
| No funding model for long-term maintenance | High | High | Decide early: sponsorship, support contracts, or the optional hosted console. Never paywall kit features. |

## 31. Things we should explicitly not build

| Don't build | Use instead |
|---|---|
| HTTP router or custom handler context | `net/http` ServeMux (or chi) |
| ORM or database abstraction | pgx + sqlc |
| DI container | Constructors in `app.go` |
| Logger | `log/slog` |
| Metrics/tracing library or backend | OpenTelemetry + any OTLP backend |
| Job queue / message broker abstraction | River; community modules for NATS/Kafka/Temporal |
| Identity server, SAML, SCIM | Ory, Keycloak, Zitadel, WorkOS via adapter |
| Migration engine | goose |
| Schema DSL | SQL |
| Package registry / upload server | Go module proxy + git-based index |
| Plugin runtime / dynamic loading | Go modules compiled in |
| Deployment platform, Kubernetes operator, Terraform generator | A Dockerfile and docs for common hosts |
| Frontend framework or UI kit | API-first; community modules for templ/htmx if wanted |
| Microservices tooling, service mesh, event bus | A modular monolith; outbox via River when needed |
| Mobile app | Existing alerting/monitoring apps |
| Dashboard-managed production config | Env vars, platform secret stores, GitOps |
| APIStock-hosted GitHub App / token storage (v1) | `gh` CLI |
| Multi-database support, multi-tenancy in core | Postgres; `orgs` module later |

## 32. Final recommended architecture

```text
 ┌───────────────────────── TOOLING (dev machine / CI, never in production) ──────────────────────────┐
 │                                                                                                    │
 │   aps CLI ── recipe resolver ──(Go proxy + sumdb)──▶ module@version/recipe   apistock-index (git)  │
 │     │                                                                                              │
 │     ├── generator: plan → render → check → preview → apply → apistock.lock                        │
 │     ├── upgrade: 3-way merge · migration copy · analyzers / go fix                                 │
 │     ├── dev: reload · compose · console (OTLP + SMTP receivers)                                    │
 │     └── forge: git · gh · CI files                                                                 │
 └─────┬──────────────────────────────────────────────────────────────────────────────────────────────┘
       │ writes plain files
       ▼
 ┌─────────────────────────────── DEVELOPER'S REPOSITORY (owned) ─────────────────────────────────────┐
 │  cmd/api · internal/app · internal/<features> · db/migrations · db/queries · apistock.yaml/lock    │
 └─────┬──────────────────────────────────────────────────────────────────────────────────────────────┘
       │ go build (imports)
       ▼
 ┌──────────────────────────────── RUNTIME (single Go binary) ────────────────────────────────────────┐
 │  app code                                                                                          │
 │    ├── official modules: postgres · auth · email · jobs(River) · auditpg · ratelimit              │
 │    ├── community modules: stripe · redis · s3 · …           (same contracts, own repos)           │
 │    └── APIStock core: app · config · httpx · health · obs · actor · audit · errs                  │
 │          └── stdlib · pgx · OpenTelemetry · slog                                                   │
 └─────┬──────────────────────┬──────────────────────┬───────────────────────────────────────────────┘
       ▼                      ▼                      ▼
   PostgreSQL            OTLP backend of       /livez /readyz  ◀── any orchestrator
   (the only required    your choice
    infrastructure)          │
                             └──(optional, separate product, later)──▶ hosted control plane
```

- **One required dependency** in production: Postgres.
- **Two v1 products**: runtime kit and CLI. The dev console arrives in P5 inside the CLI.
- **One public extension contract**: Go module + `apistock-module.yaml`.
- **One upgrade model**: libraries via `go get`, scaffolds via 3-way merge, schema via append-only migrations.

## 33. If I were building this

I would spend the first two months writing **the best-structured Go API I could, by hand**: signup, login, reset, a CRUD feature with permissions, audit in the same transaction, River jobs, slog + OTel, graceful shutdown, tests against real Postgres. I'd publish that repo on its own before announcing any framework. If experienced Go developers read it and say "this is how I'd do it", the project has earned the right to generate it. If they don't, no CLI will fix that.

Then I would pull the reusable, security-sensitive parts out into `apistock.dev` and a handful of modules, and build a CLI whose only job at first is to reproduce that app exactly. Before adding a single new module, I'd ship a second release and prove that the first release's apps upgrade cleanly, with hand edits intact. That upgrade demo is the whole pitch. Every Go starter template in existence fails it.

I would say no, for at least the first year, to the dashboard, the mobile app, a GitHub App, multi-tenancy, a second database, a custom DI system, and any module for a vendor (Stripe, Twilio, Redis). Those are exactly what a community is for, once contracts are stable. The one "platform" feature I would build early is the local `aps dev` console. It's cheap, it's offline, it needs no account, and it's the thing people will screenshot.

I'd hold the line on four rules forever: **no reflection-based wiring, no silent overwrites, Postgres is enough, and the app must keep working if APIStock disappears.** If a feature request conflicts with one of those, the feature loses.

And I would keep the name and the command exactly as they are now: APIStock and `aps`. Short, clear, and not "framework".

### Sources consulted (September 2026)

- [Encore: Best Go backend frameworks in 2026](https://encore.dev/articles/best-go-backend-frameworks) (Buffalo archival, full-stack landscape)
- [Encore: Go microservices frameworks 2026](https://encore.dev/articles/go-frameworks)
- [encoredev/encore on GitHub](https://github.com/encoredev/encore)
- [google/wire on GitHub](https://github.com/google/wire) and [pkg.go.dev](https://pkg.go.dev/github.com/google/wire) (archived Aug 2025)
- [OpenTelemetry Go Logs API and SDK reach release candidate](https://opentelemetry.io/blog/2026/go-logs-api-sdk-rc/)
- [otelslog bridge](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog)
- [riverqueue/river](https://github.com/riverqueue/river) and [brandur.org/river](https://brandur.org/river)
- [Atlas v0.38.0 release notes](https://newreleases.io/project/github/ariga/atlas/release/v0.38.0) and [Atlas Community Edition](https://atlasgo.io/community-edition)
- [PocketBase: use as framework](https://pocketbase.io/docs/use-as-framework/)
- [sqlc changelog (1.31.1)](https://docs.sqlc.dev/en/latest/reference/changelog.html)
- [shadcn/ui registry](https://ui.shadcn.com/docs/registry) and [Vercel Academy: component registries](https://vercel.com/academy/shadcn-ui/what-is-a-component-registry)
- [Go 1.24 release notes](https://go.dev/doc/go1.24) (tool directive, os.Root) and [Go 1.25 release notes](https://go.dev/doc/go1.25)
- [Using go fix to modernize Go code](https://go.dev/blog/gofix) and [Go 1.26 release notes](https://go.dev/doc/go1.26)
- [ory/kratos](https://github.com/ory/kratos) and [Ory Enterprise License](https://www.ory.com/docs/oel/kratos/intro)
- [pressly/goose](https://github.com/pressly/goose) and [goose Provider API](https://pressly.github.io/goose/blog/2023/goose-provider/)
