# CLI guide

`aps` creates apistock apps, generates code in them and runs them locally. Decisions: [ADR-0014](../adr/0014-product-shape-and-presets.md) (presets and prompts), [ADR-0021](../adr/0021-generator-operation-model.md) (generator), [ADR-0035](../adr/0035-interactive-cli.md) (interactive prompts with flag parity), [ADR-0037](../adr/0037-email-setup-and-delivery.md) (`aps add mail`), [ADR-0039](../adr/0039-resource-module-template.md) (`aps gen resource`).

## Installing

The library and CLI aren't published yet, so install `aps` from your checkout:

```bash
cd apistock/cli
go install ./cmd/aps
```

This puts `aps` in `$(go env GOPATH)/bin` (usually `~/go/bin`). If your shell then says `command not found: aps`, add that directory to your `PATH`, for example in `~/.zshrc`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Open a new terminal (or run `hash -r`) and check it works:

```bash
aps version
```

Run `go install ./cmd/aps` again after pulling changes.

## Interactive or flags: both work

Every command can be used two ways:

| Way | How | Best for |
|---|---|---|
| **Interactive** | Leave values out. In a terminal, `aps` asks for them with arrow-key menus, yes/no toggles and validated text inputs, then shows a summary to confirm | People |
| **Flags** | Pass every value as a flag. Values given by flag are never asked | Scripts, CI, AI agents, repeatable commands |

You can mix them: flags you pass skip their questions, and you're asked only for the rest.

| Rule | Behaviour |
|---|---|
| When questions appear | Only when both input and output are a terminal |
| Never ask | `--yes` (use defaults for anything not given), `--no-input` (fail if a required value is missing), `--json`, or the `CI` environment variable |
| Keys | ↑/↓ to choose, ←/→ or y/n for yes/no, Enter to confirm, Tab/Shift+Tab to move between fields, Esc or Ctrl+C to cancel |
| Cancel | Exits with code 130 and writes nothing |
| Validation | Questions check exactly what flags check, as you type |
| Look | The apistock theme ([theme](../brand/theme.md)): no borders, dim hints, lime only on the open question's `?` and the option cursor |
| Accessibility | `--plain` or `ACCESSIBLE=1` asks one plain line at a time (screen readers); `NO_COLOR=1` disables colour, and the output reads the same without it |

## `aps new`

Creates an app.

```bash
aps new                                   # asks for everything
aps new my-api                            # asks for the rest
aps new my-api --module github.com/you/my-api --local ~/code/apistock --yes
```

| Question | Flag | Default |
|---|---|---|
| App name | `<name>` (positional) | required |
| Go module path | `--module` | the app name |
| Preset (Minimal or Full) | `--preset minimal\|full` | `minimal` (Custom arrives later) |
| Tenancy (Full only): records belong to users, or to organisations | `--tenancy single\|multi` | `single` |
| apistock checkout | `--local <path>` | the checkout you run `aps` inside, if any |
| Initialise git | `--no-git` | yes |

Other flags: `--skip-tidy` (don't run `go mod tidy`), `--json`, `--yes`, `--no-input`, `--plain`.

Questions come one at a time. Each answered question folds into one line, and values you passed by flag are listed the same way, so every answer is on screen before the last question: create the app, yes or no.

```text
✓ app name … shop-api
✓ Go module path … github.com/acme/shop-api
✓ preset … full
✓ tenancy … multi
✓ apistock checkout … /Users/you/code/apistock
✓ initialise a git repository? … yes
? create shop-api in ./shop-api? … yes  no
```

Then `aps new` prints a log: one line per finished step, where things are in the new app, and the commands to run next. `--json` prints only the result.

```text
creating shop-api in ./shop-api
preset full · library ../apistock

✓ wrote 214 files
✓ ran go mod tidy
✓ initialised git

created shop-api

  api docs     http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)
  emails       http://127.0.0.1:8025 (Mailpit catches every email in development)
  ...

  next: cd shop-api
        aps dev
```

| Preset | What you get | Needs |
|---|---|---|
| **Minimal** | HTTP API with configuration, telemetry, health checks, security headers and interactive docs | Go |
| **Full** | Everything in Minimal, plus PostgreSQL, runtime settings, background jobs, email (Resend, or SMTP with `aps add mail`), authentication and platform roles, audit log, release tracking, `/ops/*` APIs, and example code: the `ping` endpoint, the `heartbeat` job and the `projects` resource | Go and Docker |

A Full app is exactly [examples/full-single](../../examples/full-single) with your name and module path ([ADR-0041](../adr/0041-full-preset-generation.md)): its database, Compose project and service name are your app's name. With `--tenancy multi` it is exactly [examples/full-multi](../../examples/full-multi) instead: data belongs to organisations, with members, one role each, invitations, personal workspaces and org-scoped projects under `/v1/orgs/{orgId}/…` ([ADR-0048](../adr/0048-organisations-v0-4.md)). Tenancy is chosen at creation; turning a single-tenant app into a multi-tenant one (`aps add orgs`) arrives in v0.5. After creating one:

```bash
cd my-api
cp .env.example .env
docker compose up -d --wait    # PostgreSQL and Mailpit
go run ./cmd/migrate
go run ./cmd/api               # or: aps dev
```

If port 5432 is taken, set `POSTGRES_PORT` in `.env` and the same port in `DATABASE_URL`. The app's README explains how to create the first admin and how to remove the examples.

Commit `apistock.lock` with the app. It records the `aps` release that created the app, the answers the templates used (name, module, preset, tenancy, email provider) and a SHA-256 of every file `aps` wrote except `go.mod` and `go.sum`. `aps upgrade` (v0.5) uses it to rebuild those files as they were and merge newer templates into your edits ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)). Don't edit it by hand.

## `aps gen job`

Generates a background job in an app created with the Full preset. The job's schedule, timeout and retries can be changed later in `/ops/jobs` without a deploy ([background jobs guide](background-jobs.md)).

```bash
aps gen job                                                     # asks for everything
aps gen job CleanupSessions                                     # asks for the rest
aps gen job CleanupSessions --schedule "0 3 * * *" --timeout 5m --max-attempts 5 --yes
aps gen job SendDigest --every 6h --description "Emails the daily digest." --disabled --yes
aps gen job RebuildIndex --on-demand --dry-run
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
| (flag only) Queue | `--queue NAME` | `default` |
| (flag only) Priority | `--priority N` (1 highest to 4) | 1 |

Other flags: `--dry-run` (show the files, write nothing), `--json`, `--allow-dirty` (allow uncommitted changes), `--yes`, `--no-input`, `--plain`.

What it creates for `CleanupSessions`:

| File | Contains |
|---|---|
| `internal/jobs/cleanupsessions/cleanupsessions.go` | `Name`, `Args`, `Worker`; write the job in `Work` |
| `internal/jobs/cleanupsessions/cleanupsessions_test.go` | A starting test |
| `internal/app/job_cleanup_sessions.go` | `jobs.Define` with the defaults you chose |
| `internal/app/jobs.go` | One `defineCleanupSessionsJob(defs, deps)` line after `//aps:anchor jobs` |

Safety checks: the app must have `internal/app/jobs.go` with the anchor; existing files are never overwritten; a job name can be registered once; the git repository must have no uncommitted changes (so the generated diff is easy to review) unless you pass `--allow-dirty`; generated Go is checked with gofmt.

## `aps gen resource`

Generates a module for records that belong to the signed-in user, in an app created with the Full preset: domain rules, use cases, a repository with hand-written SQL, `/v1/<names>` endpoints, tests and a migration. In a multi-tenant app (`aps new --tenancy multi`) records belong to an organisation instead: endpoints under `/v1/orgs/{orgId}/<names>`, every use case checks membership and a `<module>.<resource>.read` or `.write` permission with `orgs.RequireMember`, and the tests include non-members, roles without the permission and cross-organisation requests ([ADR-0048](../adr/0048-organisations-v0-4.md)). Everything it writes is your code to change ([ADR-0039](../adr/0039-resource-module-template.md)); `examples/full-single/internal/modules/projects` is exactly what it generates for the first example below, and `examples/full-multi/internal/modules/projects` what it generates there.

```bash
aps gen resource                                                        # asks for everything
aps gen resource Project name:string:unique description:text 'status:enum(active,archived)'
aps gen resource Person name:string bio:text --plural People --dry-run
aps gen resource Customer email:string:unique notes:text 'tier:enum(free,pro)' --json
```

Quote enum fields: shells treat parentheses specially.

| Question | Flag | Default |
|---|---|---|
| Resource name | `<Name>` (positional, singular): `Project`, `OrderItem` or `order-item` | required |
| Fields | positional, after the name, separated by spaces | required |
| (flag only) Plural | `--plural People` | the name with -s, -es or -ies |
| (flag only) ID prefix | `--id-prefix prj` (2 to 8 lowercase letters) | first letter and the next consonants: `prj`, `cst` |
| (flag only) Who the records belong to | `--scope user\|org` | `org` in multi-tenant apps (`tenancy: multi` in `apistock.yaml`), `user` otherwise; `org` needs the orgs module |

Flags may come before, between or after the name and fields. Other flags: `--dry-run`, `--json`, `--allow-dirty`, `--yes`, `--no-input`, `--plain`.

| Field | Means |
|---|---|
| `name:string` | 1 to 100 characters, required, sortable in lists |
| `name:string:unique` | The same, and unique among each user's records, ignoring case (409 `<resource>_<field>_taken`) |
| `notes:text` | Up to 2000 characters, optional |
| `status:enum(open,done)` | One of 2 to 20 snake_case values; the first is the default; lists can filter by it |

Field names are snake_case (up to 20 characters). A resource needs at least one string field; the first one is its title. Names every resource already has (`id`, `owner_id`, `org_id`, `created_by`, `version`, `created_at`, `updated_at`, `limit`, `cursor`, `sort`, …) and PostgreSQL reserved words (`order`, `user`, …) are refused.

An org-scoped resource declares its `<module>.<resource>.read` and `.write` permissions in its `internal/app/module_<names>.go`, and the generator adds one line after `//aps:anchor org-permissions` in `internal/app/permissions.go`, so every organisation role gets them. Change which roles hold them in `declareOrgPermissions`.

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
| `db/migrations/<version>_projects.sql` | The table, a unique index per unique field and one index per sort |
| `internal/app/modules.go` | One `registerProjects(api, mapper, svc),` line after `//aps:anchor modules` |
| `internal/app/permissions.go` (`--scope org` only) | One `projectsPermissions,` line after `//aps:anchor org-permissions` |

Then run `go run ./cmd/migrate`, `go test ./...` and `go run ./cmd/api openapi > api/openapi.json`.

Safety checks: the app must have `internal/app/modules.go` with the anchor inside `errors.Join`, the auth module and `db/migrations`; existing modules and files are never overwritten; a resource can be registered once; the migration always sorts after the existing ones; the git repository must be clean unless `--allow-dirty`; names and field types come from allowlists and generated Go names are checked for clashes, so no input reaches the code unchecked; generated Go is checked with gofmt.

## `aps gen migration`

Creates an empty SQL migration in an app created with the Full preset, for database changes that aren't a new resource: a column, an index, a data fix. (`aps gen resource` creates its own migration.)

```bash
aps gen migration add_customer_phone
aps gen migration AddCustomerPhone --dry-run      # the same file name; writes nothing
aps gen migration                                  # asks for the name
```

| Question | Flag | Default |
|---|---|---|
| Migration name | `<name>` (positional): `add_customer_phone`, `AddCustomerPhone` or `add-customer-phone` | required |

Other flags: `--dry-run`, `--json`, `--allow-dirty`, `--yes`, `--no-input`, `--plain`.

It creates `db/migrations/<version>_add_customer_phone.sql` with a short comment and an empty `-- +goose Up` section. The version is the current UTC time, such as `20260916083000`, or one more than the newest migration's when that is later, so the new migration always runs last. There is no Down section: migrations only go forward ([ADR-0005](../adr/0005-database-strategy.md)).

Then write the SQL under `-- +goose Up`, run `go run ./cmd/migrate` and `go test ./...` (tests migrate a fresh database with every file).

- **Write the SQL before migrating.** Goose records an empty migration as applied, so SQL added to it afterwards never runs.
- **Edit it only until it is released.** Locally, `docker compose down -v` resets a database that already ran it; once released, add a new migration instead.
- A statement that contains semicolons, such as a function body, goes between `-- +goose StatementBegin` and `-- +goose StatementEnd`.
- Don't write `+goose` in other comments: goose reads any comment line containing it as an annotation and rejects the file.

Safety checks: the app must have `db/migrations`; the name may only use letters, digits, hyphens and underscores (at most 60), so it can't reach a path or the SQL; an existing file is never overwritten; the git repository must be clean unless `--allow-dirty`.

## `aps add mail`

Sets up email in an app created with the Full preset: Resend or any SMTP server. Run it again to switch provider. Full walkthrough: [email guide](email.md).

```bash
aps add mail                                             # asks for everything
aps add mail --provider resend --yes                     # Resend; add RESEND_API_KEY to .env yourself
aps add mail --smtp-host smtp.postmarkapp.com --smtp-username <token>   # SMTP; asks for the password
aps add mail --provider smtp --dry-run                   # show what would change
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
| `internal/app/infra_mail.go` | Replaced with the provider's configuration and constructor |
| `.env.example` | The block between `# aps:begin mail` and `# aps:end mail` holds the provider's variables |
| `.env` | Updated if it exists, or created from `.env.example` (mode 0600) when there are values to save; values already there are kept |
| `apistock.yaml` | `mail: resend` or `mail: smtp` |
| `apistock.lock` | The provider and the new hashes of the files above that `aps` tracks (apps created before v0.5 keep their lock as it is) |
| `go.mod` | Requires the provider module (with a `replace` to your apistock checkout when the app uses one), then `go mod tidy` |

After confirming, it prints numbered next steps: where to get the Resend key and verify your domain (or which SMTP variables are left), how to set the sender with `PUT /ops/settings/mail.from_email`, and how to send a test email with `POST /ops/mail/test`. The sender name, address and reply-to are runtime settings, so they're never asked here.

Safety checks: the app must have `internal/app/mail.go` and the `.env.example` block; the git repository must be clean unless `--allow-dirty`; `.env` must be ignored by git before a secret is saved in it; secret values are never printed or included in `--json` output. Running it with the provider already in place changes nothing.

## `aps dev`

Builds and runs the app in the current directory, rebuilding when files change and loading `.env` (variables already set in the environment win).

In an app with a database (Full preset), before the first start it:

1. Creates `.env` from `.env.example` (mode 0600) when there is none, and fills an empty `AUTH_ENCRYPTION_KEYS` with a random development key ([ADR-0043](../adr/0043-two-factor-authentication.md)).
2. Checks Docker, and that the services' host ports are free: `POSTGRES_PORT`, `MAILPIT_SMTP_PORT` and `MAILPIT_WEB_PORT` (ports held by the app's own running services are fine). A taken port names the `.env` line that moves it.
3. Starts PostgreSQL and Mailpit from the app's `compose.yaml` with `docker compose up -d --wait`.
4. Applies migrations (`go run ./cmd/migrate`) and runs seed data (`go run ./cmd/seed`). The first run prints the administrator's password once ([ADR-0042](../adr/0042-development-seed-data.md)); later runs change nothing.
5. Prints the API, docs and email inbox addresses, then starts the app.

While it runs, a changed or new migration is applied before the restart; if it fails, the previous version keeps running. Services keep running after `aps dev` stops, so the next start is fast: `docker compose down` stops them, and `docker compose down -v` also deletes the database.

| Flag | Default |
|---|---|
| `--observability` | off. Also starts Grafana (`grafana/otel-lgtm`, the `observability` profile in `compose.yaml`) on `GRAFANA_PORT` (3000) and sets `OTEL_EXPORTER_OTLP_ENDPOINT` for the app, so its traces, metrics and logs appear there. Works in Minimal apps too |
| `--no-services` | start services. Skips Docker and uses `DATABASE_URL` and `MAILPIT_SMTP_ADDR` from `.env` as they are; migrations and seed data still run |
| `--no-reload` | reload on change |
| `--interval` | 500ms between change checks |

Without Docker, a Full app stops with a message: install and start Docker, or point `DATABASE_URL` at an existing PostgreSQL and use `--no-services`. Without the CLI, the same steps are `cp .env.example .env`, `docker compose up -d --wait`, `go run ./cmd/migrate`, `go run ./cmd/seed` and `go run ./cmd/api`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed (for example, the directory already exists) |
| 2 | Invalid usage: a bad flag or value, or a required value missing without a terminal |
| 130 | Cancelled at a prompt; nothing was written |

## JSON output

`--json` prints only a machine-readable result on stdout and never prompts.

```json
{"name": "SendDigest", "definition": "send_digest", "files": ["internal/jobs/senddigest/senddigest.go", "…"], "dry_run": false}
```

Commands, flags, exit codes and JSON fields are public API from CLI 1.0 ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)).
