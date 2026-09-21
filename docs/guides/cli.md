# CLI guide

`orb` creates gorbital apps, generates code in them and runs them locally. Decisions: [ADR-0014](../adr/0014-product-shape-and-presets.md) (presets and prompts), [ADR-0021](../adr/0021-generator-operation-model.md) (generator), [ADR-0035](../adr/0035-interactive-cli.md) (interactive prompts with flag parity), [ADR-0037](../adr/0037-email-setup-and-delivery.md) (`orb add mail`), [ADR-0039](../adr/0039-resource-module-template.md) (`orb gen resource`).

## Installing

```bash
go install gorbital.dev/cli/cmd/orb@latest
```

This puts `orb` in `$(go env GOPATH)/bin` (usually `~/go/bin`). If your shell then says `command not found: orb`, add that directory to your `PATH`, for example in `~/.zshrc`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Open a new terminal (or run `hash -r`) and check it works:

```bash
orb version
```

`orb version --json` prints the version, recipe and library version for scripts. Run the same `go install` command again to update `orb`.

Two other ways to get `orb`:

| Way | How |
|---|---|
| A release binary | Download the archive for your system from [GitHub releases](https://github.com/gorbital/gorbital/releases), [verify it](#verifying-a-release-binary), and put `orb` on your `PATH` |
| From a checkout, to work on gorbital itself | `git clone https://github.com/gorbital/gorbital.git`, then `cd gorbital/cli && go install ./cmd/orb`. Run it again after pulling changes. Create apps against the checkout with `orb new --local <path>` ([local development](local-development.md)) |

Build `orb` with the latest Go patch release. `orb` writes every file through `os.Root` so nothing escapes the app, and Go releases before 1.26.5 have `os.Root` escapes that were fixed later. The `go.mod` directive stays at `go 1.26.0` ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)), so `go install` accepts an older toolchain. When it does, `orb version` prints a warning and `orb doctor` warns in its `orb` check.

### Verifying a release binary

Each GitHub release has archives, a `checksums.txt` covering all of them, a Sigstore bundle for that file (`checksums.txt.sigstore.json`) and SLSA build provenance. The release workflow signs with its GitHub identity, so no key is involved. Before you run a downloaded binary, check that the release workflow of `gorbital/gorbital` built it from a `cli/v*` tag:

```bash
# 1. The checksums were signed by the release workflow at a cli/v* tag.
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/gorbital/gorbital/\.github/workflows/release-cli\.yml@refs/tags/cli/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. Your archive matches its signed checksum (on macOS: shasum -a 256 --ignore-missing -c checksums.txt).
sha256sum --ignore-missing -c checksums.txt

# 3. Optional: GitHub's build provenance for the archive.
gh attestation verify orb_<version>_<os>_<arch>.tar.gz \
  --repo gorbital/gorbital \
  --signer-workflow gorbital/gorbital/.github/workflows/release-cli.yml
```

Step 1 must print `Verified OK` and step 2 must print `OK` for your archive. If either fails, don't run the binary; report it as described in [SECURITY.md](../../SECURITY.md). Releases are drafts until a maintainer publishes them, and the release job only runs for a tagged commit on `main` after a maintainer approves it in the `release` environment.

## Interactive or flags: both work

Every command can be used two ways:

| Way | How | Best for |
|---|---|---|
| **Interactive** | Leave values out. In a terminal, `orb` asks for them with arrow-key menus, yes/no toggles and validated text inputs, then shows a summary to confirm | People |
| **Flags** | Pass every value as a flag. Values given by flag are never asked | Scripts, CI, AI agents, repeatable commands |

You can mix them: flags you pass skip their questions, and you're asked only for the rest.

| Rule | Behaviour |
|---|---|
| When questions appear | Only when both input and output are a terminal |
| Never ask | `--yes` (use defaults for anything not given), `--no-input` (fail if a required value is missing), `--json`, or the `CI` environment variable |
| Keys | ↑/↓ to choose, ←/→ or y/n for yes/no, Enter to confirm, Tab/Shift+Tab to move between fields, Esc or Ctrl+C to cancel |
| Cancel | Exits with code 130 and writes nothing |
| Validation | Questions check exactly what flags check, as you type |
| Look | The gorbital theme ([theme](../brand/theme.md)): no borders, dim hints, lime only on the open question's `?` and the option cursor |
| Accessibility | `--plain` or `ACCESSIBLE=1` asks one plain line at a time (screen readers); `NO_COLOR=1` disables colour, and the output reads the same without it |

## `orb new`

Creates an app.

```bash
orb new                                   # asks for everything
orb new my-api                            # asks for the rest
orb new my-api --module github.com/you/my-api --yes
orb new my-api --local ~/code/gorbital    # use a gorbital checkout instead of the published library
orb new my-api --no-start                 # create it, but don't start orb dev
```

| Question | Flag | Default |
|---|---|---|
| App name | `<name>` (positional) | required |
| Go module path | `--module` | the app name |
| Preset (Minimal or Full) | `--preset minimal\|full` | `minimal` (Custom arrives later) |
| Tenancy (Full only): records belong to users, or to organisations | `--tenancy single\|multi` | `single` |
| Initialise git | `--no-git` | yes |

Other flags: `--local <path>` (use a gorbital checkout instead of the published library; default: the checkout you run `orb` inside, if any), `--skip-tidy` (don't run `go mod tidy`), `--json`, `--yes`, `--no-input`, `--plain`.

With `--preset full`, `orb new` writes sign-in into the app: `internal/modules/auth` (and `internal/modules/orgs` with `--tenancy multi`), every migration in `db/migrations`, `cmd/api/main.go` importing those packages, and `gorbital.lock` recording where each was copied from. It is the app's code from the first commit, and there is one shape of Full app: since v0.2.2 no flag keeps these modules in the library instead ([ADR-0092](../adr/0092-what-the-framework-owns.md)). The API, the database and the behaviour are the same as importing them from the library, and the primitives they call — password hashing, session tokens, TOTP, passkey and OAuth verification — stay there ([The code in your repo](the-code-in-your-repo.md)).

Questions come one at a time. Each answered question folds into one line, and values you passed by flag are listed the same way, so every answer is on screen before the last question: create the app, yes or no.

```text
✓ app name … shop-api
✓ Go module path … github.com/acme/shop-api
✓ preset … full
✓ tenancy … multi
✓ gorbital checkout … /Users/you/code/gorbital
✓ initialise a git repository? … yes
? create shop-api in ./shop-api? … yes  no
```

Then `orb new` prints a log: one line per finished step, where things are in the new app, and the commands to run next. In a terminal it then starts `orb dev` in the new app, which opens the Dev Portal, so the first run ends in the browser ([ADR-0077](../adr/0077-generators-hub-first-run-and-project-settings.md)); `--no-start` (and `--yes`, `--no-input`, `--json`) skip that, `--start` forces it. `--json` prints only the result.

```text
creating shop-api in ./shop-api
preset full · library gorbital.dev v0.1.0

✓ wrote 63 files
✓ ran go mod tidy
✓ initialised git

created shop-api

  api docs     http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)
  main.go      cmd/api/main.go runs the app on gorbital.Main; your code goes in internal/modules
  modules      orb gen module <Name> <field:type>... adds a table and its API
  emails       http://127.0.0.1:3100/mail (the Dev Portal catches every email in development)
  ...

  next: cd shop-api
        orb dev
```

| Preset | What you get | Needs |
|---|---|---|
| **Minimal** | HTTP API with configuration, telemetry, health checks, security headers and interactive docs | Go |
| **Full** | Everything in Minimal, plus PostgreSQL, runtime settings, background jobs, email (Resend, or SMTP with `orb add mail`), authentication and platform roles, audit log, release tracking, `/ops/*` APIs, and two example modules: `projects` (as `orb gen module` writes it) and `ping` (a public endpoint with a runtime setting and a feature flag) | Go and Docker |

A Full app runs on `gorbital.Main` (ADR-0083): `cmd/api/main.go` adds the built-in modules (sign-in with `authhttp`, `/ops` with `opshttp`, client flags, email events, and organisations with `orgshttp` in a multi-tenant app) and the app's own modules from `internal/modules/modules.gen.go`, with the app's migrations from `db/migrations`. `cmd/api/mail.go` and `cmd/api/storage.go` hold the email provider and S3-compatible storage. It is exactly [examples/full-single](../../examples/full-single) with your name and module path ([ADR-0041](../adr/0041-full-preset-generation.md)): its database, Compose project and service name are your app's name. With `--tenancy multi` it is exactly [examples/full-multi](../../examples/full-multi) instead: data belongs to organisations, with members, one role each, invitations, personal workspaces and org-scoped projects under `/v1/orgs/{orgId}/…` ([ADR-0048](../adr/0048-organisations-v0-4.md)). Tenancy is chosen at creation; `orb add orgs` turns a single-tenant app into a multi-tenant one later. Apps created by orb v0.1 keep the v0.1 layout (`internal/app`, [examples/v0.1/full-single](../../examples/v0.1/full-single)): every command below works in them as documented for v0.1, and they upgrade within their layout. Minimal apps keep composing core packages directly (roadmap decision D17). After creating one:

```bash
cd my-api
git add -A && git commit -m "Create my-api"   # orb new doesn't commit; orb gen and orb add need a clean tree
orb dev                                      # .env, PostgreSQL, the mail catcher, migrations, seed data, the API
```

Without `orb dev`, export `.env` yourself: the app reads environment variables, not the file.

```bash
cp .env.example .env           # then set AUTH_ENCRYPTION_KEYS: echo "k1:$(openssl rand -base64 32)"
docker compose up -d --wait    # PostgreSQL
set -a; . ./.env; set +a       # in each terminal, and again after editing .env
go run ./cmd/api migrate
go run ./cmd/api seed          # the development administrator
go run ./cmd/api
```

If port 5432 is taken, set `POSTGRES_PORT` in `.env` and the same port in `DATABASE_URL`. The app's README explains how to create the first admin and how to remove the examples.

Commit `gorbital.lock` with the app. It records the `orb` release that created the app, the answers the templates used (name, module, preset, tenancy, email provider, and `layout: v0.2` for apps on `gorbital.Main`; no layout means v0.1) and a SHA-256 of every file `orb` wrote except `go.mod` and `go.sum`. `orb upgrade` uses it to rebuild those files as they were and merge newer templates into your edits ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)). Don't edit it by hand.

## `orb add storage`

Chooses where a Full preset app keeps files ([storage guide](storage.md), [ADR-0075](../adr/0075-file-storage.md)): `--driver local|s3|spaces|r2|minio` with `--endpoint`, `--region`, `--bucket`, `--access-key` and `--public-url`. It rewrites the `storage` block of `.env.example`, sets the values in `.env` (put `STORAGE_SECRET_KEY` there yourself) and, for `minio`, adds the MinIO service to `compose.yaml`, which `orb dev` then starts (`MINIO_PORT`, `MINIO_CONSOLE_PORT`).

```bash
orb add storage --driver minio
orb add storage --driver s3 --region eu-west-1 --bucket acme-files --access-key AKIA… --yes
```

## `orb gen job`

Generates a background job in an app on the v0.1 layout (`internal/app/jobs.go`); in an app on `gorbital.Main` it refuses with exit status 2 and points at a module's `Jobs` ([Modules and routes](modules-and-routes.md)). The job's schedule, timeout and retries can be changed later in `/ops/jobs` without a deploy ([background jobs guide](background-jobs.md)).

```bash
orb gen job                                                     # asks for everything
orb gen job CleanupSessions                                     # asks for the rest
orb gen job CleanupSessions --schedule "0 3 * * *" --timeout 5m --max-attempts 5 --yes
orb gen job SendDigest --every 6h --description "Emails the daily digest." --disabled --yes
orb gen job RebuildIndex --on-demand --dry-run
orb gen job PingHook --kind http --url https://example.com/hook --body '{"ping":true}' --every 5m --yes
orb gen job PurgeDrafts --kind sql --sql "DELETE FROM drafts WHERE updated_at < now() - interval '30 days'" --yes
orb gen job WeeklyDigest --kind email --to ops@example.com --subject "Weekly digest" --text "All is well." --schedule "0 9 * * 1" --yes
orb gen job NightlyChain --kind dispatch --dispatch PurgeDrafts --yes
```

| Question | Flag | Default |
|---|---|---|
| Job name | `<Name>` (positional): `CleanupSessions`, `cleanup-sessions` or `cleanup_sessions` | required |
| What does it do? | `--description` | `<Name> job.` |
| When should it run? (schedule, interval, on demand) | one of `--schedule`, `--every`, `--on-demand` | schedule |
| Schedule (daily 03:00, hourly, Mondays 09:00, monthly, or custom cron) | `--schedule "0 3 * * *"` (5-field cron in UTC or `@daily`, `@hourly`, …) | `0 3 * * *` |
| Interval (5m, 15m, 30m, 1h, 6h, or custom) | `--every 15m` (at least 1m) | `1h` |
| Timeout per attempt (30s, 1m, 5m, 15m, 1h) | `--timeout 5m` (1s to 24h) | `1m` |
| Attempts before giving up (1, 3, 5, 10, 25) | `--max-attempts N` (1 to 100) | 5 |
| Enable the job now? | `--disabled` | enabled |
| What does the job do? | `--kind custom` (a `Work` method to write), `http` (`--method`, `--url`, `--body` as JSON: the answer must be 2xx), `sql` (`--sql`: one statement on the app's pool), `email` (`--to`, `--subject`, `--text`: through the app's mailer, the job ID as idempotency key), `dispatch` (`--dispatch <Name>`: starts that job). The generated `Work` is ordinary Go; the definition carries an `//orb:job` marker the Dev Portal reads back until the worker is edited by hand ([ADR-0071](../adr/0071-job-kinds-and-ejection.md)) | `custom` |
| (flag only) Queue | `--queue NAME` | `default` |
| (flag only) Priority | `--priority N` (1 highest to 4) | 1 |

Other flags: `--dry-run` (show the files, write nothing), `--json`, `--allow-dirty` (allow uncommitted changes), `--yes`, `--no-input`, `--plain`.

What it creates for `CleanupSessions`:

| File | Contains |
|---|---|
| `internal/jobs/cleanupsessions/cleanupsessions.go` | `Name`, `Args`, `Worker`; write the job in `Work` |
| `internal/jobs/cleanupsessions/cleanupsessions_test.go` | A starting test |
| `internal/app/job_cleanup_sessions.go` | `jobs.Define` with the defaults you chose |
| `internal/app/jobs.go` | One `defineCleanupSessionsJob(defs, deps)` line after `//orb:anchor jobs` |

The job name is public API: record it with `go test ./internal/app -run TestPublicSurface -update`, which updates `api/surface.json` ([stability](stability.md)).

Safety checks: the app must have `internal/app/jobs.go` with the anchor; existing files are never overwritten; a job name can be registered once; the git repository must have no uncommitted changes (so the generated diff is easy to review) unless you pass `--allow-dirty`; generated Go is checked with gofmt.

## `orb gen resource`

In an app on `gorbital.Main`, `orb gen resource` runs [`orb gen module`](#orb-gen-module) with the same name, fields and flags (`--scope org` is the old name of `--scope tenant`; without `--scope` the module is owned by users, or by the tenant in a multi-tenant app) and says so; what follows describes apps on the v0.1 layout.

Generates a module for records that belong to the signed-in user, in an app created with the Full preset: domain rules, use cases, a repository with hand-written SQL, `/v1/<names>` endpoints, tests and a migration. In a multi-tenant app (`orb new --tenancy multi`) records belong to an organisation instead: endpoints under `/v1/orgs/{orgId}/<names>`, every use case checks membership and a `<module>.<resource>.read` or `.write` permission with `orgs.RequireMember`, and the tests include non-members, roles without the permission and cross-organisation requests ([ADR-0048](../adr/0048-organisations-v0-4.md)). Everything it writes is your code to change ([ADR-0039](../adr/0039-resource-module-template.md)); `examples/v0.1/full-single/internal/modules/projects` is exactly what it generates for the first example below, and `examples/v0.1/full-multi/internal/modules/projects` what it generates there. This describes an app on the v0.1 layout; in an app on `gorbital.Main` it runs [`orb gen module`](#orb-gen-module) (with `--org` by default in a multi-tenant app).

```bash
orb gen resource                                                        # asks for everything
orb gen resource Project name:string:unique description:text 'status:enum(active,archived)'
orb gen resource Person name:string bio:text --plural People --dry-run
orb gen resource Customer email:string:unique notes:text 'tier:enum(free,pro)' --json
```

Quote enum fields: shells treat parentheses specially.

| Question | Flag | Default |
|---|---|---|
| Resource name | `<Name>` (positional, singular): `Project`, `OrderItem` or `order-item` | required |
| Fields | positional, after the name, separated by spaces | required |
| (flag only) Plural | `--plural People` | the name with -s, -es or -ies |
| (flag only) ID prefix | `--id-prefix prj` (2 to 8 lowercase letters) | first letter and the next consonants: `prj`, `cst` |
| (flag only) Who the records belong to | `--scope user\|tenant` | `tenant` in multi-tenant apps (`tenancy: multi` in `gorbital.yaml`), `user` otherwise; `tenant` needs the orgs module, and `org` is its old name. `public` and `custom` need an app on `gorbital.Main` ([Resource access scopes](resource-access.md)) |

Flags may come before, between or after the name and fields. Other flags: `--dry-run`, `--json`, `--allow-dirty`, `--yes`, `--no-input`, `--plain`.

| Field | Means |
|---|---|
| `name:string` | 1 to 100 characters, required, sortable in lists |
| `name:string:unique` | The same, and unique among each user's records, ignoring case (409 `<resource>_<field>_taken`) |
| `notes:text` | Up to 2000 characters, optional |
| `status:enum(open,done)` | One of 2 to 20 snake_case values; the first is the default; lists can filter by it |

Field names are snake_case (up to 20 characters). A resource needs at least one string field; the first one is its title. Names every resource already has (`id`, `owner_id`, `org_id`, `created_by`, `version`, `created_at`, `updated_at`, `limit`, `cursor`, `sort`, …) and PostgreSQL reserved words (`order`, `user`, …) are refused.

A resource declares its `<module>.<resource>.read` and `.write` permissions in its `internal/app/module_<names>.go`, and the generator adds one line to `internal/app/permissions.go`. For an org-scoped resource the line goes after `//orb:anchor org-permissions`, so every organisation role gets them; change which roles hold them in `declareOrgPermissions`. For a user-scoped resource it goes after `//orb:anchor user-permissions`, so the `user` role every user holds gets them and every use case checks them: sessions always pass, and an [API key](api-keys.md) only within its scopes (ADR-0058). A key scoped to `.read` gets 403 `forbidden` on create, update and delete.

What it creates for `Project`:

| File | Contains |
|---|---|
| `internal/modules/projects/domain/` | `Project`, `ProjectFields`, `Changes`, validation and errors, with tests |
| `internal/modules/projects/usecase/` | Create, get, list, update and delete for the signed-in owner, audit events, tests on PostgreSQL |
| `internal/modules/projects/repository/` | One SQL file per operation and tests on PostgreSQL |
| `internal/modules/projects/delivery/projects.go` | `POST`, `GET`, `PATCH` and `DELETE` under `/v1/projects` |
| `internal/modules/projects/module.go` | Wires the layers |
| `internal/app/module_projects.go` | Builds the module and maps its error codes |
| `internal/app/projects_test.go` | An end-to-end HTTP test, including another user's requests getting 404 |
| `db/migrations/<version>_projects.sql` | The table, a unique index per unique field and one index per sort; with `--scope org` in an app that ran `orb add rls`, forced row-level security and the `org_isolation` policy |
| `internal/app/modules.go` | One `registerProjects(api, mapper, svc),` line after `//orb:anchor modules` |
| `internal/app/permissions.go` | One `projectsPermissions,` line after `//orb:anchor org-permissions` (`--scope org`) or `//orb:anchor user-permissions` (`--scope user`) |

Then run `go run ./cmd/migrate`, `go run ./cmd/api openapi --dir api`, `go test ./internal/app -run TestPublicSurface -update` (records the resource's error codes, audit actions and permissions in `api/surface.json`, [stability](stability.md)) and `go test ./...`.

Safety checks: the app must have `internal/app/modules.go` with the anchor inside `errors.Join`, the auth module and `db/migrations`; existing modules and files are never overwritten; a resource can be registered once; the migration always sorts after the existing ones; the git repository must be clean unless `--allow-dirty`; names and field types come from allowlists and generated Go names are checked for clashes, so no input reaches the code unchecked; generated Go is checked with gofmt.

## `orb gen migration`

Creates an empty SQL migration in an app created with the Full preset, for database changes that aren't a new resource: a column, an index, a data fix. (`orb gen resource` creates its own migration.)

```bash
orb gen migration add_customer_phone
orb gen migration AddCustomerPhone --dry-run      # the same file name; writes nothing
orb gen migration                                  # asks for the name
```

| Question | Flag | Default |
|---|---|---|
| Migration name | `<name>` (positional): `add_customer_phone`, `AddCustomerPhone` or `add-customer-phone` | required |

Other flags: `--dry-run`, `--json`, `--allow-dirty`, `--yes`, `--no-input`, `--plain`.

It creates `db/migrations/<version>_add_customer_phone.sql` with a short comment and an empty `-- +goose Up` section. The version is the current UTC time, such as `20260916083000`, or one more than the newest migration's when that is later, so the new migration always runs last. There is no Down section: migrations only go forward ([ADR-0005](../adr/0005-database-strategy.md)).

Then write the SQL under `-- +goose Up`, run `go run ./cmd/api migrate` (`go run ./cmd/migrate` in a v0.1 app) and `go test ./...` (tests migrate a fresh database with every file).

- **Write the SQL before migrating.** Goose records an empty migration as applied, so SQL added to it afterwards never runs.
- **Edit it only until it is released.** Locally, `docker compose down -v` resets a database that already ran it; once released, add a new migration instead.
- A statement that contains semicolons, such as a function body, goes between `-- +goose StatementBegin` and `-- +goose StatementEnd`.
- Don't write `+goose` in other comments: goose reads any comment line containing it as an annotation and rejects the file.

Safety checks: the app must have `db/migrations`; the name may only use letters, digits, hyphens and underscores (at most 60), so it can't reach a path or the SQL; an existing file is never overwritten; the git repository must be clean unless `--allow-dirty`.

## `orb gen modules`

Writes `internal/modules/modules.gen.go` in an app on `gorbital.Main` (v0.2): the function `All`, listing every directory under `internal/modules` whose package declares `func Module() gorbital.Module`, sorted by name, for `gorbital.WithModules(modules.All()...)` ([Your main.go](main-go.md#the-module-list)).

```bash
orb gen modules
orb gen modules --dry-run      # says whether the file would change; writes nothing
go generate ./internal/modules # the same: the generated file carries the directive
```

Flags: `--dry-run`, `--json`, `--no-input` (it never prompts). It needs no clean git tree: it writes only its own generated file, and nothing when the file is up to date.

Modules are found by parsing Go files (`go/parser`), never by building or running code: test files, `testdata` and directories starting with `.` or `_` are skipped; a `Module` function with parameters, a receiver or another return type isn't a module, and neither is a built-in module the app owns, such as `internal/modules/auth`, which `main.go` adds itself. `orb dev` runs it before every build when the app already has `modules.gen.go`, so adding a module directory is enough; apps without the file (v0.1 apps) are left alone.

## `orb gen module`

Generates a module in an app on `gorbital.Main` (v0.2): records that belong to the signed-in user, or, with `--scope`, to a tenant, to everyone, or to whoever the module's own `policy.go` says ([Resource access scopes](resource-access.md)), in `internal/modules/<names>/` with four layers and **one file per operation** in each, the route table in `delivery/routes.go`, a migration, tests, and the module added to `modules.gen.go` ([ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#3-app-layout)). Everything it writes is your code; [Generating code](generating-code.md) goes through it file by file. Shelfie's `internal/modules/shelves` is exactly what the first example writes ([chapter 9](../examples/shelfie/09-generators.md)).

```bash
orb gen module Shelf name:string:unique description:text 'visibility:enum(private,shared)' --plural Shelves
orb gen module Reader name:string 'nickname:string?' --dry-run --diff
orb gen module ClubBook title:string:unique 'author:string?' 'status:enum(proposed,reading,finished)' note:text --org
orb gen module                                          # asks for the name and fields
```

Quote enum fields and optional strings: shells treat parentheses and `?` specially.

| Question | Flag | Default |
|---|---|---|
| Record name | `<Name>` (positional, singular): `Shelf`, `OrderItem` or `order-item` | required |
| Fields | positional, after the name | required |
| (flag only) Plural | `--plural Shelves` | the name with -s, -es or -ies; the module, table and route come from it |
| (flag only) ID prefix | `--id-prefix shl` (2 to 8 lowercase letters) | first letter and the next consonants |
| (flag only) Who may read and write the records | `--scope user\|tenant\|public\|custom` | `tenant` in an app with a tenancy, `user` otherwise. `--org` and `--scope org` are the old names of `--scope tenant` ([Resource access scopes](resource-access.md)) |

Other flags: `--dry-run`, `--diff` (prints the plan as a unified diff), `--json`, `--allow-dirty`, `--yes`, `--no-input`, `--plain`.

| Field | Means |
|---|---|
| `name:string` | 1 to 100 characters, required, sortable |
| `name:string:unique` | The same, unique among each user's records (with `--org`, each organisation's), ignoring case (409 `<record>_<field>_taken`) |
| `nickname:string?` | 0 to 100 characters, optional, sortable; can't be unique |
| `notes:text` | Up to 2000 characters, optional |
| `status:enum(open,done)` | One of 2 to 20 snake_case values, the first by default; lists filter by it |

The first required string is the title the tests sort by. Names are checked as `orb gen resource` checks them, and also against the names the generated code uses (`page`, `item`, `domain`…), so it always compiles.

What it writes for `Shelf`:

| File | Contains |
|---|---|
| `internal/modules/shelves/module.go` | `Module()`: the name, the error mappings, the `shelves.shelf.read` and `.write` permissions (the `user` role), and `Routes` wiring the layers |
| `…/domain/shelf.go`, `errors.go`, `shelf_test.go` | The record, its fields and changes, validation with `ValidationError`, the errors, table-driven tests |
| `…/usecase/service.go`, `ports.go` | The service (store, audit, logger, clock), the `Store` port, the owner check and audit helper |
| `…/usecase/create_shelf.go`, `get_shelf.go`, `list_shelves.go`, `update_shelf.go`, `delete_shelf.go` | One operation each: keyset pagination with `sort` and `cursor`, updates with an optimistic `version` in a transaction |
| `…/repository/store.go` | The store on the pool or a transaction, the column list, the row scanner and the unique-constraint errors |
| `…/repository/insert_shelf.go`, `select_shelf.go`, `select_shelves.go`, `update_shelf.go`, `delete_shelf.go` | One SQL statement (or one per sort) each |
| `…/delivery/routes.go` | The route table: `gorbital.Post/Get/Patch/Delete` on `/v1/shelves` with operation IDs, statuses and `guard.Permission` |
| `…/delivery/responses.go`, `create_shelf.go`, … | The response type and field errors, then each operation's input, output and handler |
| `…/shelves_test.go` | HTTP tests through [gorbitaltest](testing-with-gorbitaltest.md): create, rules, deny by default, other users get 404, read-only API keys get 403, pages, versions, audit events |
| `db/migrations/<version>_shelves.sql` | The table with `CHECK` constraints, a unique index per unique field, an index per sort, and a Down section |
| `internal/modules/architecture_test.go` | The layer rules of every module, when the app has none |
| `internal/modules/modules.gen.go` | Rewritten with the new module ([`orb gen modules`](#orb-gen-modules)) |

Then `go run ./cmd/api migrate` (`orb dev` does it), `go run ./cmd/api openapi --dir api` and `go test ./...`. When `cmd/api` doesn't pass `modules.All()` to `gorbital.Main`, the next steps say to add it.

**With `--org`** the same files are written for an organisation's records ([Generating code](generating-code.md#organisations); Shelfie's `internal/modules/clubbooks` is the second example, [chapter 8](../examples/shelfie/08-book-clubs.md)):

| What changes | With `--org` |
|---|---|
| Routes | Under `/v1/orgs/{orgId}/<names>`, each with `guard.OrgMember(usecase.PermRead)` or `(usecase.PermWrite)`: a caller who isn't a member gets 404 `org_not_found`, a role without the permission 403 `forbidden` |
| Permissions | Organisation permissions, `OrgRoles: owner, admin, member`, so every member reads and writes; an API key only within its scopes |
| Use cases and SQL | Take the organisation ID from the path, check it is the one the guard authorized, and filter every statement on `org_id`; the record has `OrgID` and `CreatedBy`, and responses `created_by`. Another organisation's record is 404 `<record>_not_found` |
| Migration | `org_id text NOT NULL`, `UNIQUE (org_id, id)`, unique and sort indexes led by `org_id`, the foreign key to `orgs` with `ON DELETE CASCADE` (added when the migration runs with the organisations module's tables, so another module's test app migrates without them), and the `org_isolation` row-level security policy when the app has row-level security: a `db/migrations/*_row_level_security.sql` migration or `rls: true` in `gorbital.yaml` |
| Tests | Real accounts through `gorbitaltest.App.SignUp` in an app with `authhttp` and `orgshttp`: each user's personal workspace, other users get `org_not_found` on every route, another organisation's record is not found, read-only API keys get 403, pages, versions, audit events with `org_id`, and purging an organisation deleting its records |

The plan never edits `main.go`. When `cmd/api` doesn't mention `orgshttp`, the first next step says to add `gorbital.WithModules(orgshttp.Module(auth))`, where `auth` is the authenticator passed to `gorbital.WithAuth`; `gorbital.New` refuses routes with `guard.OrgMember` without it, so tests that build the app from `modules.All()` need it too. `--json` adds `"scope": "org"` and `"row_level_security": true` when the migration carries the policy.

Safety checks: the app must be on `gorbital.Main` (`gorbital.dev/gorbital` in `go.mod` or `modules.gen.go`) and have `db/migrations` as a Go package (`migrations.FS`); no file or module directory is overwritten; the git repository must be clean unless `--allow-dirty`. In an app on the v0.1 layout it stops and points at `orb gen resource`.

## `orb gen middleware`

Generates middleware or a guard, with a table-driven test, in an app on `gorbital.Main` ([Guards and middleware](guards-and-middleware.md)). It writes new files only and prints the line that puts them to use: it never edits `routes.go`, `module.go` or `main.go`.

```bash
orb gen middleware RequireClientVersion --module books          # middleware for a module's routes
orb gen middleware ActiveSubscription --module books --guard    # a guard and its error
orb gen middleware TenantHeader --global                        # middleware for every route
```

| Kind | Writes | Put it to use |
|---|---|---|
| `--module <name>` | `internal/modules/<name>/delivery/<name>.go` and its test: `func RequireClientVersion(next http.Handler) http.Handler` with its rule in `checkRequireClientVersion` | `gorbital.Use(RequireClientVersion)` on a group or route in `routes.go`, or `delivery.RequireClientVersion` in `Module.Middleware` |
| `--module <name> --guard` | The same file names: `func ActiveSubscription() gorbital.RouteOption` (`guard.New`, named `active_subscription`), its rule, and `ErrActiveSubscriptionRefused` | `ActiveSubscription()` on routes in `routes.go`; map the error in `module.go` with the line it prints (403 `active_subscription_refused`) |
| `--global` | `internal/middleware/<name>.go` and its test, and `doc.go` when the package has none | `gorbital.WithMiddleware(middleware.TenantHeader)` in `cmd/api/main.go` |

Flags: `--module`, `--global`, `--guard`, `--dry-run`, `--diff`, `--json`, `--allow-dirty`, `--no-input` (it never prompts). Exactly one of `--module` and `--global`; `--guard` needs `--module`. The rule lets every request through until you write it, so adding the middleware changes nothing by itself. A name the package already declares is refused.

## `orb add mail`

Sets up email in an app created with the Full preset: Resend or any SMTP server. Run it again to switch provider. Full walkthrough: [email guide](email.md).

```bash
orb add mail                                             # asks for everything
orb add mail --provider resend --yes                     # Resend; add RESEND_API_KEY to .env yourself
orb add mail --smtp-host smtp.postmarkapp.com --smtp-username <token>   # SMTP; asks for the password
orb add mail --provider smtp --dry-run                   # show what would change
```

| Question | Flag | Default |
|---|---|---|
| How should the app send email? (Resend, recommended; SMTP) | `--provider resend\|smtp` (any `--smtp-*` flag implies smtp) | `resend` |
| Resend API key (optional, hidden) | none: saved only to `.env` | add it to `.env` later |
| SMTP server (optional) | `--smtp-host` | add it to `.env` later |
| Port and encryption (587 STARTTLS, 465 TLS, 2525 STARTTLS, 25 none) | `--smtp-port`, `--smtp-tls` | 587, `starttls` (`tls` for 465) |
| SMTP username (optional) | `--smtp-username` | none |
| SMTP password (optional, hidden, asked after a username) | none: saved only to `.env` | add it to `.env` later |

Secrets are the one exception to "every question has a flag": they would end up in shell history. Type them at the hidden prompt, or put them in `.env` or the environment.

Other flags: `--dry-run`, `--json`, `--allow-dirty`, `--skip-tidy` (don't run `go mod tidy`), `--yes`, `--no-input`, `--plain`.

What it changes:

| File | Change |
|---|---|
| `cmd/api/mail.go` (apps on `gorbital.Main`) | Replaced with the provider's `mailer` function, which `main.go` passes to `gorbital.WithMailerFunc`, and `mailProvider`, which `opshttp.MailProvider` reports in `GET /ops/mail` |
| `internal/app/infra_mail.go` (v0.1 layout) | Replaced with the provider's configuration and constructor |
| `internal/app/infra_mail_test.go` (v0.1 layout) | Replaced with the provider's tests and the fixtures the rest of the app's tests use, so `go test ./...` passes with either provider |
| `.env.example` | The block between `# orb:begin mail` and `# orb:end mail` holds the provider's variables; the `# aps:` markers of apps generated before the rename are read too and rewritten as `# orb:` |
| `.env` | Updated if it exists, or created from `.env.example` when there are values to save; values already there are kept. It is always left with mode 0600: an existing `.env` that other users could read (as `cp .env.example .env` makes it) is narrowed, with a warning |
| `gorbital.yaml` | `mail: resend` or `mail: smtp` |
| `gorbital.lock` | The provider and the new hashes of the files above that `orb` tracks (a format v1 lock, from development builds before `orb upgrade`, is kept as it is) |
| `go.mod` | Requires the provider module (with a `replace` to your gorbital checkout when the app uses one), then `go mod tidy` |

After confirming, it prints numbered next steps: where to get the Resend key and verify your domain (or which SMTP variables are left), how to set the sender with `PUT /ops/settings/mail.from_email`, and how to send a test email with `POST /ops/mail/test`. The sender name, address and reply-to are runtime settings, so they're never asked here.

Safety checks: the app must have `cmd/api/mail.go` or `internal/app/mail.go`, and the `.env.example` block; the git repository must be clean unless `--allow-dirty`; `.env` must be ignored by git before a secret is saved in it; secret values are never printed or included in `--json` output. Running it with the provider already in place changes nothing.

## `orb add orgs`

Turns a single-tenant Full app into a multi-tenant one, on branch `orb-add-orgs`: organisations with members, one role each, invitations and personal workspaces, and projects under `/v1/orgs/{orgId}/projects` ([ADR-0048](../adr/0048-organisations-v0-4.md), [ADR-0050](../adr/0050-upgrades-and-adding-features.md)).

```bash
orb add orgs --dry-run     # what would change, per file
orb add orgs               # apply on branch orb-add-orgs
```

It merges the multi-tenant app's files of the app's layout into yours the way `orb upgrade` merges a release: files you never edited are replaced (in an app on `gorbital.Main`, `main.go` gains `gorbital.WithModules(orgshttp.Module(auth))` and the example `projects` module becomes the organisation one), your edits are merged or shown as conflicts. Then it adds migrations after your existing ones:

| Migration | What it does |
|---|---|
| `<version>_orgs.sql` (v0.1 layout only) | Creates `orgs`, `org_members` and `org_invitations`, followed by the later organisation migrations. In an app on `gorbital.Main` the organisations module brings these migrations itself, under their released versions — see the warning below |
| `<version>_orgs_convert.sql` | Gives every account a personal workspace it owns (a deleted account's workspace is deleted too, purged 30 days after the account's deletion), then moves each project into its owner's workspace: `org_id` and `created_by` replace `owner_id`. The table is changed in place, so columns you added stay; this step is skipped if `projects` no longer has `owner_id` |

In an app on `gorbital.Main` the report ends with a warning, because the organisations module's migrations keep the versions v0.1 apps hold them under (`20260916000001` and `20260918000002`), older than `20260918000070`, the newest built-in migration every such database has already run. goose refuses a migration older than the database's version, so an **existing** database fails at the next migrate with `detected 2 missing (out-of-order) migrations lower than database version`. A database created after the change, and the tests, are unaffected. The warning names the two ways out, and `--json` carries them as `migration_order_warning` (`versions`, `newest`, `summary`, `error`, `options`):

- a development database: `docker compose down -v && docker compose up -d --wait`, then `go run ./cmd/api migrate`;
- a database you have to keep: apply the two migrations by hand and record them in `goose_db_version` — [Adding organisations to a database that already exists](../start/organisations.md#adding-organisations-to-a-database-that-already-exists) has the SQL and why it is safe for these two files.

Without conflicts it updates `go.mod`, builds, regenerates `api/openapi.json`, records `api/surface.json` and commits `Add organisations`. Then run `go test ./...`, apply the migrations (`orb dev`, or `go run ./cmd/api migrate` in each environment — `go run ./cmd/migrate` in a v0.1 app) and merge the branch. Set `orgs.invitation_url` before inviting people.

Modules you generated stay owned by users and keep working; the command lists them. To move one to organisations, generate it again (`orb gen module --org`, or `orb gen resource --scope org` in a v0.1 app) and move its data.

Other flags: `--json`, `--skip-tidy`, `--skip-build`. Safety checks: the app must be in git with no uncommitted changes, and on this release (run `orb upgrade` first). An app that already has organisations is left alone.

## `orb add rls`

Turns on row-level security in a multi-tenant app: a fifth isolation layer, in PostgreSQL, under the four organisations already have ([ADR-0061](../adr/0061-row-level-security.md), [guide](row-level-security.md)).

```bash
orb add rls --dry-run      # the files it would write
orb add rls                # write them in the working tree
go run ./cmd/api migrate   # go run ./cmd/migrate in a v0.1 app
```

| File | Change |
|---|---|
| `db/migrations/<version>_row_level_security.sql` | A copy of `db/row_level_security.sql`: forces row-level security, with the `org_isolation` policy, on every table with `org_id NOT NULL` except `org_members` and `org_invitations` |
| `gorbital.yaml` | `rls: true`, so `orb gen module --org` (and `orb gen resource --scope org`) adds the policy to new modules' migrations |
| `gorbital.lock` | `inputs.rls` and the new hash of `gorbital.yaml`, so `orb upgrade` keeps the line |

The app already sets the organisation on every database connection, so no code changes. It prints next steps: connect as a role that isn't a superuser and has no `BYPASSRLS` (PostgreSQL applies no policy to those), migrate, run `orb doctor` and the tests, and commit.

Other flags: `--json`. Safety checks: the app must be multi-tenant (run `orb add orgs` first), on this release (run `orb upgrade` first), and in git with no uncommitted changes. Running it again changes nothing.

## `orb upgrade`

Brings the files `orb` wrote into your app up to this release, on a branch, without losing your edits ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)).

```bash
orb upgrade --dry-run          # what would change, per file; writes nothing
orb upgrade                    # apply on branch orb-upgrade/<version>
orb upgrade --from <commit>    # apps whose lock records no release or commit name the gorbital commit that created them
```

It rebuilds every file exactly as the release recorded in `gorbital.lock` wrote it, checks each against the hash in the lock, and merges per file. Both sides are the app's own layout's templates: an app on the v0.1 layout is rebuilt from and merged with the v0.1-layout templates this release still carries, so it never receives files of the `gorbital.Main` layout, and the report says `layout: v0.1` (`"layout"` in `--json`) with a pointer to `orb upgrade --layout v0.2`, the opt-in move:

| Your file | The new release | Result |
|---|---|---|
| Never edited | Changed | `update`: takes the new template |
| Edited | Unchanged | Kept as you left it |
| Edited | Changed elsewhere in the file | `merged` |
| Edited | Changed the same lines | `conflict`: `<<<<<<< yours` … `>>>>>>> gorbital <version>` markers |
| Missing | New | `create` |
| Never edited | Removed | `delete` |
| Edited or deleted by you | Removed or changed | `kept`, with a note |

A file whose rebuilt content doesn't match the lock is compared as yours against the release, so it can conflict but is never overwritten. Migrations are never merged: new ones are added with their released names, and yours stay as they are. Files `orb gen` created aren't tracked, so they're never touched.

Without conflicts it then updates `go.mod` (new requirements and the new library version, then `go mod tidy`), runs `go build ./...`, regenerates `api/openapi.json` (with the Postman collection and `llms.txt`), records the app's public names in `api/surface.json` (`go test ./internal/modules -run TestPublicSurface -update` on `gorbital.Main`, `./internal/app` in a v0.1 app; review what changed in the commit) and commits `Upgrade gorbital to <version>`. Run your tests (database tests need `orb dev` or `docker compose up -d --wait`) and merge the branch. With conflicts it exits with code 1, commits nothing, and lists the files to resolve and the commands to finish.

Where earlier releases come from: with an gorbital checkout (`--local`, the checkout your `go.mod` replaces the library with, or the one you run in), `git archive` of the release's tag or commit. Otherwise the `gorbital.dev/cli` module from the Go module proxy, verified by the checksum database: `orb upgrade` refuses when `GOSUMDB` is `off` or names a database other than `sum.golang.org`, or when `GONOSUMDB`, `GOPRIVATE` or `GOINSECURE` covers the module (patterns match as Go matches them, with or without a trailing slash). Templates are only rendered as text; nothing downloaded is run.

A checkout must be the top of its own git repository, outside the app's repository, and the release must be a commit of the gorbital repository (its `go.mod` is `module gorbital.dev`, or `apistock.dev` before the rename). `--local` pointing inside the app is refused. A detected checkout inside the app, such as a `replace` to a copy committed in the app, is passed over, so the release comes from the module proxy instead. Otherwise a commit to the app could supply both the earlier templates and the lock hashes that prove them. The name, module, preset, tenancy and mail provider read from `gorbital.lock` or `gorbital.yaml` are checked with the rules `orb new` uses before anything is rendered.

| Flag | Default |
|---|---|
| `--from` | the release in `gorbital.lock`. Needed for apps with a format v1 lock (development builds before `orb upgrade`) and apps created by development builds without a recorded commit: pass that gorbital commit |
| `--local` | detected, as above |
| `--dry-run`, `--json` | off |
| `--skip-tidy` | run `go mod tidy` |
| `--skip-build` | build, regenerate `api/openapi.json`, record `api/surface.json` and commit |

Safety checks: the app must be in git with no uncommitted changes, and the branch `orb-upgrade/<version>` must not exist yet.

## `orb upgrade --layout v0.2`

Moves a v0.1 app (a composition root in `internal/app`) to the v0.2 layout: `cmd/api/main.go` on `gorbital.Main`, the app's modules in `internal/modules`, the built-in ones from `gorbital.dev/gorbital` ([ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md), walkthrough: [Upgrading a v0.1 app](../examples/recipes/upgrading-a-v0.1-app.md)). It is opt-in: a v0.1 app works on the v0.2 library without it.

```bash
orb upgrade                        # first: the app must be on this release's v0.1 templates
orb upgrade --layout v0.2 --dry-run   # the plan, line by line; writes nothing
orb upgrade --layout v0.2             # convert the app in the working tree
```

The report has one line per decision, and `UPGRADE-v0.2.md` in the app repeats them with the details:

| Line | What it means |
|---|---|
| `library` | Generated code you never changed, deleted: the library runs it now (`authhttp`, `opshttp`, `flagshttp`, `mailevents`, `orgshttp`, `gorbital.Main`) |
| `template` | Written from the v0.2 templates, such as an example module nobody changed |
| `converted` | One of your modules, kept where it is: its `huma.Register` calls become `gorbital` routes, and the error mappings and permissions of `internal/app/module_<name>.go` move into its `Module` value |
| `kept` | A built-in module you changed, now the app's own code: the library's module copied into `internal/modules`, with your changes moved into the copy, and recorded in `gorbital.lock` ([The code in your repo](the-code-in-your-repo.md)). The line names the [options and hooks](configuring-sign-in.md) that could replace the change |
| `carried` | A change moved to where it belongs in the new layout, such as your middleware into `gorbital.WithStack` in `main.go` |
| `manual` | A change orb can't make. It stops the move; `--allow-manual` converts the rest and keeps those files in `_upgrade-v0.1/`, which the go command ignores |
| `follow-up` | Something left for you that doesn't stop the move, such as a test of the old composition root |

Then it runs `go mod tidy`, `gofmt`, `go build ./...`, exports `api/` again and records `api/surface.json`. Nothing is committed, and `git restore . && git clean -fd` undoes it. In `api/`, `openapi.json` gains `x-gorbital-guards` on every route and its `/ops` instance example takes the app's own name, while `surface.json` and `openapi.baseline.json` are rewritten, because the built-in modules' error codes, roles and jobs belong to the library from then on and the file records only the app's own names.

Your migration history is untouched, the copies of the library's migrations in `db/migrations` included: `gorbital.Migrate` reads a copy with the same version and identical content as the same migration, so a database the v0.1 app migrated has nothing to apply.

| Flag | Default |
|---|---|
| `--dry-run`, `--json`, `--diff` | off |
| `--allow-dirty` | refuse a git repository with uncommitted changes |
| `--allow-manual` | stop at the changes orb can't make |
| `--yes`, `--no-input`, `--plain` | ask for confirmation in a terminal |
| `--skip-tidy`, `--skip-build` | run them |

Safety checks: the app must be in git, created by `orb new` (it needs `gorbital.lock`), on the v0.1 layout and on this release's v0.1 templates — run `orb upgrade` and commit it first. Minimal apps keep the v0.1 layout and are refused.

## `orb routes`

Lists every route of the app: method, path, operation ID, module, guards, middleware, handler and where the route is registered. That includes the routes of library modules, such as `authhttp`'s `/v1/auth/…` and `opshttp`'s `/ops/…`, which have no source; `--app` lists only the routes in the app's source. It changes nothing.

```bash
orb routes                                   # builds the app's OpenAPI document with go run ./cmd/api openapi
orb routes --openapi api/openapi.json        # reads a document instead: no build
orb routes --app                             # only the app's own routes, not the library modules'
orb routes --module books --json
orb routes --public                          # only routes that need no sign-in
```

```text
METHOD  PATH                     OPERATION                    MODULE   GUARDS                                                          HANDLER        SOURCE
GET     /ops/audit               ops-list-audit-events        -        authenticated                                                   -              -
…
GET     /v1/books                books-get-v1-books           books    authenticated, permission:books.book.read                       h.listBooks    internal/modules/books/delivery/routes.go:31
POST    /v1/books                books-post-v1-books          books    authenticated, permission:books.book.write, rate_limit:30/1m0s  h.createBook   internal/modules/books/delivery/routes.go:28
…
GET     /version                 get-version                  -        public                                                          -              -

137 routes, 19 public
note: 127 of 137 routes have no source position: registered by a library module (such as authhttp or opshttp), or through code that builds the path at run time
note: orb routes --app lists only the routes in the app's source
```

| Column | Comes from |
|---|---|
| METHOD, PATH, OPERATION | The OpenAPI document |
| SCOPE | Who the route's records belong to: the app's tenant, `user`, `public` or `custom`, from the module's record in `gorbital.yaml`, or from the route's own guards when there is none. A dash means neither knows, as for a library module's routes ([Resource access scopes](resource-access.md)) |
| GUARDS | `x-gorbital-guards`: `authenticated` or `public`, then each guard in the order it runs. A route without it (a library route) shows `public` when it has no security requirement |
| MIDDLEWARE | Shown when a route has any: `Module.Middleware`, then each group's `gorbital.Use` (outer first), then the route's, as written in the source |
| MODULE, HANDLER, SOURCE | The app's Go source, read with `go/parser`: the directory under `internal/modules` (or its `gorbital.Module` `Name`), the handler expression, and the `gorbital.Get`/`Post`/… call's file and line |

Routes are matched to the source by method and path, resolving `r.Group(prefix)` variables and inline groups in the same function, or by a literal `gorbital.OperationID`. A route whose path is built at run time, or that a library module registers, has no source and a note says how many; `--app` leaves those routes out, and that note with them. In an app on the v0.1 layout, the `huma.Register` calls with a `huma.Operation` literal are found the same way; the document has no `x-gorbital-guards`, so GUARDS shows `?` (or `public` for routes without a security requirement) and no middleware is listed.

Flags: `--json`, `--openapi <file>`, `--app`, `--module <name>`, `--public`, `--no-input`. Exit code 1 when the document can't be built or read (the message says how to check the build).

The `--json` output is public API (here `orb routes --app --json`):

```json
{
  "schemaVersion": 1,
  "app": "shelfie",
  "source": "export",
  "guards_known": true,
  "total": 10,
  "public": 0,
  "routes": [
    {
      "method": "POST",
      "path": "/v1/books",
      "operation_id": "books-post-v1-books",
      "summary": "Add a book to your shelf",
      "tags": ["Books"],
      "module": "books",
      "handler": "h.createBook",
      "source": {"file": "internal/modules/books/delivery/routes.go", "line": 28},
      "handler_source": {"file": "internal/modules/books/delivery/create_book.go", "line": 20},
      "guards": ["authenticated", "permission:books.book.write", "rate_limit:30/1m0s"],
      "middleware": [],
      "public": false,
      "deprecated": false
    }
  ],
  "warnings": []
}
```

| Field | Type | Meaning |
|---|---|---|
| `source` | string | `export` (built with `go run ./cmd/api openapi`), `file` (`--openapi`), or `app` (the Dev Portal read the running app's `/openapi.json`) |
| `guards_known` | bool | `false` when the document has no `x-gorbital-guards` (a v0.1 app) |
| `total`, `public` | number | Counts of the listed routes, after `--app`, `--module` and `--public` |
| `routes[].source`, `handler_source` | object or `null` | `file` (slash-separated, relative to the app) and `line`; `null` when not found |
| `routes[].guards`, `middleware`, `tags` | array of strings | Always present, possibly empty |
| `warnings` | array of strings | What couldn't be found, for people; don't parse them |

## `orb doctor`

Checks the app in the current directory and says what to fix. It changes nothing ([ADR-0051](../adr/0051-operations-v0-5.md)).

```bash
orb doctor           # every check
orb doctor --fast    # skip the checks that build the app
orb doctor --json    # for scripts and agents
```

```text
orb doctor · shop-api (full, single tenancy)

  ok    go             go1.26.8; go.mod needs 1.26.0
  ok    git            installed
  ok    orb            v0.1.0 built with go1.26.8
  ok    gorbital.yaml  full preset, single tenancy
  warn  docker         Docker isn't running or isn't installed
                       fix: start Docker Desktop (or Docker Engine with Compose v2): orb dev runs PostgreSQL and Mailpit in it
  ok    gorbital.lock  from orb v0.1.0; 3 of 214 files gorbital wrote are edited or removed
  ok    library        gorbital.dev v0.1.0
  ok    anchors        every line generators insert at is in place
  ok    .env           has every variable .env.example has
  ok    api files      openapi.json, postman_collection.json and llms.txt match the code
  warn  database       2 migrations pending
                       fix: go run ./cmd/migrate (orb dev runs them)

  0 failed, 2 warnings
```

| Check | Fails when | Warns when |
|---|---|---|
| `go`, `git`, `docker` | Go isn't installed | Go is older than `go.mod` needs; git isn't installed; Docker isn't running (Full preset) |
| `orb` | | `orb` was built with a Go release older than 1.26.5, which lacks `os.Root` security fixes; reinstall it with the latest Go patch release |
| `gorbital.yaml`, `gorbital.lock` | Either is unreadable, or the lock was written by a newer `orb` | The lock is missing, format v1 (from a development build before `orb upgrade`), or from an older `orb` (run `orb upgrade`) |
| `library` | A `replace` directive points at something that isn't an gorbital checkout | `go.mod` doesn't require `gorbital.dev` |
| `anchor` (Full preset on the v0.1 layout) | A line generators insert after is gone: `//orb:anchor modules`, `//orb:anchor jobs`, `//orb:anchor user-permissions`, `//orb:anchor org-permissions` (multi-tenant), or the mail block in `.env.example` | |
| `.env` (Full preset) | It holds secrets and git doesn't ignore it | It's missing, git doesn't ignore it, or it lacks variables `.env.example` has |
| `api files` | | `api/openapi.json`, `postman_collection.json` or `llms.txt` doesn't match the code |
| `configuration`, `database` (Full preset) | The app's configuration doesn't load; the database ran migrations the code doesn't have | The database is unreachable, or migrations are pending |
| `modules` (apps on `gorbital.Main`) | `modules.gen.go` is missing or stale: a module directory isn't listed, or a listed one is gone | A directory under `internal/modules` has Go files but no `func Module() gorbital.Module` (a `Module` that takes arguments, which `main.go` adds on its own line, and the built-in modules the app owns aren't reported) |
| `ejected` (apps on `gorbital.Main`, one per built-in module the app owns, as `gorbital.lock` records them) | The module's directory is gone | The library's package at the version `go.mod` requires differs from the one copied (its SHA-256), with the changelog entries that name it; or it can't be compared |
| `stack` (apps on `gorbital.Main`) | | A `gorbital.WithStack` in `cmd/api` leaves out `Recover` or `Auth` (a function literal, or a function declared in `cmd/api`, that never names them and doesn't use `Default()`); a stack built any other way can't be checked, and the warning points at the one `gorbital.New` logs at start |
| `timeout` (apps on `gorbital.Main`) | `APP_REQUEST_TIMEOUT` isn't a duration, or isn't shorter than the server's 60s write timeout: the app refuses to start | It is `0` (no deadline) or shorter than a second |
| `scopes` (apps on `gorbital.Main` whose `gorbital.yaml` records module scopes) | | A `tenant` module's **generated** repository queries don't mention the tenant column, or a `custom` module's `policy.go` still returns `gorbital.ErrNotImplemented`. It is a static check over generated files: it can't see SQL added later, a query built at run time, or a view that widens the rows, so a clean run is a reminder and not a proof ([Resource access scopes](resource-access.md)) |
| `row-level security` (Full preset) | | Row-level security is on and the database role is a superuser or has `BYPASSRLS`, a table's row-level security isn't forced, or an organisation table has no policy ([row-level security](row-level-security.md)) |

Values from `.env` are never printed. The database checks run the app's own `go run ./cmd/migrate --status --json`, or `go run ./cmd/api migrate --status --json` in an app on `gorbital.Main`, whose status covers the merged history of the library's and the app's migrations; so `orb` needs no database driver. The JSON result says the app's `layout` (`main` or `v0.1`). Exit code 1 when any check fails.

## `orb dev`

Builds and runs the app in the current directory, rebuilding when files change and loading `.env` (variables already set in the environment win).

In an app with a database (Full preset), before the first start it:

1. Creates `.env` from `.env.example` (mode 0600) when there is none, and fills an empty `AUTH_ENCRYPTION_KEYS` with a random development key ([ADR-0043](../adr/0043-two-factor-authentication.md)).
2. Checks Docker, and that the services' host ports are free: `POSTGRES_PORT` (and `MAILPIT_SMTP_PORT`, `MAILPIT_WEB_PORT` when `compose.yaml` still has Mailpit; ports held by the app's own running services are fine). A taken port names the `.env` line that moves it.
3. Starts PostgreSQL (and Mailpit when defined) from the app's `compose.yaml` with `docker compose up -d --wait`, and its own mail catcher on `DEV_MAIL_SMTP_ADDR` when `MAIL_DELIVERY` is `devmail` ([ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)).
4. Applies migrations (`go run ./cmd/migrate`) and runs seed data (`go run ./cmd/seed`). The first run prints the administrator's password once ([ADR-0042](../adr/0042-development-seed-data.md)); later runs change nothing.
5. Prints the API, docs and email inbox addresses, then starts the app.

In every app whose `.env.example` declares `DEV_CONSOLE_TOKEN` and that runs with `APP_ENV=development`, `orb dev` also generates a random dev console token (256 bits) for the run, passes it to the app in its environment only (never to `.env` or any file), and prints the [dev console APIs](dev-console.md) address and the token. Rebuilds keep the token; the next run gets a new one. A `DEV_CONSOLE_TOKEN` already set in `orb dev`'s own environment is passed instead and not printed.

While it runs, a changed or new migration is applied before the restart; if it fails, the previous version keeps running. Services keep running after `orb dev` stops, so the next start is fast: `docker compose down` stops them, and `docker compose down -v` also deletes the database.

It also serves the [Dev Portal](dev-portal.md) at http://127.0.0.1:3100 and opens it in your browser ([ADR-0066](../adr/0066-dev-portal.md)): the app's state and output as it happens, restart, stop and start, and the app's dev console APIs through a proxy; more screens arrive with each phase of the [Dev Portal roadmap](../dev-portal-roadmap.md). The printed link holds a token that is new on every run and never written to disk; the portal answers only this machine. Its port is checked before anything starts, like the services' ports.

| Flag | Default |
|---|---|
| `--observability` | off. Also starts Grafana (`grafana/otel-lgtm`, the `observability` profile in `compose.yaml`) on `GRAFANA_PORT` (3000) and sets `OTEL_EXPORTER_OTLP_ENDPOINT` for the app, so its traces, metrics and logs appear there. Works in Minimal apps too. Grafana receives metrics over OTLP and doesn't scrape the app; to check the Prometheus endpoint locally, set `METRICS_ADDR=127.0.0.1:9464` in `.env` and open http://127.0.0.1:9464/metrics ([production](production.md#prometheus-metrics)) |
| `--no-services` | start services. Skips Docker and uses `DATABASE_URL` (and `MAILPIT_SMTP_ADDR`) from `.env` as they are; the mail catcher, migrations and seed data still run |
| `--no-reload` | reload on change |
| `--interval` | 500ms between change checks |
| `--portal-port` | `DEV_PORTAL_PORT` from `.env` or the environment, else 3100 |
| `--no-portal` | serve the Dev Portal |
| `--no-open` | open the Dev Portal in a browser (never in CI or when output isn't a terminal) |
| `--tunnel quick\|named` | no tunnel. Also exposes the app, and only the app, on a public HTTPS address with your own `cloudflared` ([Tunnel](../dev-portal/tunnel.md), [ADR-0086](../adr/0086-dev-portal-tunnel.md)): `quick` gets a random `trycloudflare.com` URL, new on every run (webhooks, phones); `named` runs your tunnel from the Cloudflare dashboard with `CLOUDFLARE_TUNNEL_TOKEN` (or `CLOUDFLARE_TUNNEL_TOKEN_FILE`) from `.env` (sign-in callbacks, passkeys). Development only. Stopped with `orb dev`; a quick tunnel follows the app to a new port |
| `--tunnel-hostname` | `ORB_TUNNEL_HOSTNAME`, else the hostname saved from the Dev Portal. The named tunnel's public hostname, such as `dev-api.example.com`; only with `--tunnel named` |

Without Docker, a Full app stops with a message: install and start Docker, or point `DATABASE_URL` at an existing PostgreSQL and use `--no-services`. Without the CLI, the same steps are `cp .env.example .env` (and a key in `AUTH_ENCRYPTION_KEYS`), `docker compose up -d --wait`, `set -a; . ./.env; set +a` to export `.env`, `go run ./cmd/migrate`, `go run ./cmd/seed` and `go run ./cmd/api`.

With `--tunnel`, `orb dev` prints the tunnel's lines as they happen (`orb: tunnel https://… → http://127.0.0.1:8080`, `connected`, `reachable`, cloudflared's errors, `tunnel stopped`); it has no JSON output, and `GET /_portal/api/tunnel` is the machine-readable status. It exits with status 2 for a wrong `--tunnel` value or `--tunnel-hostname` without `--tunnel named`, and 1 when the tunnel can't start: cloudflared isn't installed (the install steps for your system follow), `APP_ENV` isn't development, or the named tunnel's token or hostname is missing or invalid.

The port check listens on `127.0.0.1` only. On macOS, a program listening on all addresses (such as another Docker project's PostgreSQL on `0.0.0.0:5432`) isn't detected, and `docker compose up` then fails with `Bind for 0.0.0.0:5432 failed: port is already allocated`; move the port the same way.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed (for example, the directory already exists) |
| 2 | Invalid usage: a bad value, or a required value missing without a terminal (an unknown flag is 1) |
| 130 | Cancelled at a prompt; nothing was written |

## JSON output

`--json` prints only a machine-readable result on stdout and never prompts. Every object starts with `schemaVersion`:

```json
{"schemaVersion": 1, "name": "SendDigest", "definition": "send_digest", "files": ["internal/jobs/senddigest/senddigest.go", "…"], "dry_run": false}
```

| Command | Fields after `schemaVersion` |
|---|---|
| `orb new` | `name`, `module`, `dir`, `preset`, `tenancy`, `files` (count) |
| `orb gen job` | `name`, `definition`, `files`, `dry_run` |
| `orb gen resource` | `name`, `module`, `route`, `table`, `scope`, `files`, `dry_run`, `row_level_security` (when the migration has the policy) |
| `orb gen migration` | `name`, `version`, `file`, `dry_run` |
| `orb gen modules` | `file`, `modules`, `changed`, `dry_run` |
| `orb gen module` | `name`, `module`, `route`, `table`, `scope`, `permissions`, `migration`, `files`, `dry_run` (`orb gen resource` in an app on `gorbital.Main` prints the same) |
| `orb gen middleware` | `name`, `kind` (`module`, `guard` or `global`), `module`, `package`, `file`, `test`, `files`, `wire`, `dry_run` |
| `orb routes` | `app`, `source`, `guards_known`, `total`, `public`, `routes` (see [`orb routes`](#orb-routes)), `warnings` |
| `orb add mail` | `provider`, `already_configured`, `files`, `env_variables`, `modules`, `dry_run` |
| `orb add rls` | `name`, `already_on`, `migration`, `files`, `dry_run` |
| `orb add orgs`, `orb upgrade` | `name`, `from`, `to`, `up_to_date`, `branch`, `changes` (`path`, `action`, `note`), `conflicts`, `unproven`, `committed`, `dry_run`, `layout`, `user_scoped_modules` and `migration_order_warning` (`orb add orgs`) |
| `orb doctor` | `app`, `preset`, `tenancy`, `layout`, `checks` (`name`, `status`, `detail`, `fix`), `failures`, `warnings` |
| `orb version` | `version`, `recipe`, `library` |

Commands, flags, exit codes and JSON fields are public API from `v1.0.0` ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)). Within `schemaVersion` 1, fields are only added; check the version before reading the rest. The shape of each output is recorded in `cli/internal/cli/testdata/json` and checked by `TestJSONOutputs` ([stability](stability.md), [ADR-0054](../adr/0054-api-freeze-and-scaffold-compatibility.md)).
