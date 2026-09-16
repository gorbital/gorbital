# CLI guide

`orb` creates gorbital apps, generates code in them and runs them locally. Decisions: [ADR-0014](../adr/0014-product-shape-and-presets.md) (presets and prompts), [ADR-0021](../adr/0021-generator-operation-model.md) (generator), [ADR-0035](../adr/0035-interactive-cli.md) (interactive prompts with flag parity), [ADR-0037](../adr/0037-email-setup-and-delivery.md) (`orb add mail`), [ADR-0039](../adr/0039-resource-module-template.md) (`orb gen resource`).

## Installing

The library and CLI aren't published yet, so install `orb` from your checkout:

```bash
cd gorbital/cli
go install ./cmd/orb
```

This puts `orb` in `$(go env GOPATH)/bin` (usually `~/go/bin`). If your shell then says `command not found: orb`, add that directory to your `PATH`, for example in `~/.zshrc`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Open a new terminal (or run `hash -r`) and check it works:

```bash
orb version
```

`orb version --json` prints the version, recipe and library version for scripts. Run `go install ./cmd/orb` again after pulling changes.

Build `orb` with the latest Go patch release. `orb` writes every file through `os.Root` so nothing escapes the app, and Go releases before 1.26.5 have `os.Root` escapes that were fixed later. The `go.mod` directive stays at `go 1.26.0` ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)), so `go install` accepts an older toolchain. When it does, `orb version` prints a warning and `orb doctor` warns in its `orb` check.

### Verifying a release binary

Once `orb` binaries are published, each GitHub release has archives, a `checksums.txt` covering all of them, a Sigstore bundle for that file (`checksums.txt.sigstore.json`) and SLSA build provenance. The release workflow signs with its GitHub identity, so no key is involved. Before you run a downloaded binary, check that the release workflow of `gorbital/gorbital` built it from a `cli/v*` tag:

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
orb new my-api --module github.com/you/my-api --local ~/code/gorbital --yes
```

| Question | Flag | Default |
|---|---|---|
| App name | `<name>` (positional) | required |
| Go module path | `--module` | the app name |
| Preset (Minimal or Full) | `--preset minimal\|full` | `minimal` (Custom arrives later) |
| Tenancy (Full only): records belong to users, or to organisations | `--tenancy single\|multi` | `single` |
| gorbital checkout | `--local <path>` | the checkout you run `orb` inside, if any |
| Initialise git | `--no-git` | yes |

Other flags: `--skip-tidy` (don't run `go mod tidy`), `--json`, `--yes`, `--no-input`, `--plain`.

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

Then `orb new` prints a log: one line per finished step, where things are in the new app, and the commands to run next. `--json` prints only the result.

```text
creating shop-api in ./shop-api
preset full · library ../gorbital

✓ wrote 214 files
✓ ran go mod tidy
✓ initialised git

created shop-api

  api docs     http://localhost:8080/docs (localhost, not 127.0.0.1, for passkeys)
  emails       http://127.0.0.1:8025 (Mailpit catches every email in development)
  ...

  next: cd shop-api
        orb dev
```

| Preset | What you get | Needs |
|---|---|---|
| **Minimal** | HTTP API with configuration, telemetry, health checks, security headers and interactive docs | Go |
| **Full** | Everything in Minimal, plus PostgreSQL, runtime settings, background jobs, email (Resend, or SMTP with `orb add mail`), authentication and platform roles, audit log, release tracking, `/ops/*` APIs, and example code: the `ping` endpoint, the `heartbeat` job and the `projects` resource | Go and Docker |

A Full app is exactly [examples/full-single](../../examples/full-single) with your name and module path ([ADR-0041](../adr/0041-full-preset-generation.md)): its database, Compose project and service name are your app's name. With `--tenancy multi` it is exactly [examples/full-multi](../../examples/full-multi) instead: data belongs to organisations, with members, one role each, invitations, personal workspaces and org-scoped projects under `/v1/orgs/{orgId}/…` ([ADR-0048](../adr/0048-organisations-v0-4.md)). Tenancy is chosen at creation; `orb add orgs` turns a single-tenant app into a multi-tenant one later. After creating one:

```bash
cd my-api
git add -A && git commit -m "Create my-api"   # orb new doesn't commit; orb gen and orb add need a clean tree
orb dev                                      # .env, PostgreSQL and Mailpit, migrations, seed data, the API
```

Without `orb dev`, export `.env` yourself: the app reads environment variables, not the file.

```bash
cp .env.example .env           # then set AUTH_ENCRYPTION_KEYS: echo "k1:$(openssl rand -base64 32)"
docker compose up -d --wait    # PostgreSQL and Mailpit
set -a; . ./.env; set +a       # in each terminal, and again after editing .env
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/api
```

If port 5432 is taken, set `POSTGRES_PORT` in `.env` and the same port in `DATABASE_URL`. The app's README explains how to create the first admin and how to remove the examples.

Commit `gorbital.lock` with the app. It records the `orb` release that created the app, the answers the templates used (name, module, preset, tenancy, email provider) and a SHA-256 of every file `orb` wrote except `go.mod` and `go.sum`. `orb upgrade` uses it to rebuild those files as they were and merge newer templates into your edits ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)). Don't edit it by hand.

## `orb gen job`

Generates a background job in an app created with the Full preset. The job's schedule, timeout and retries can be changed later in `/ops/jobs` without a deploy ([background jobs guide](background-jobs.md)).

```bash
orb gen job                                                     # asks for everything
orb gen job CleanupSessions                                     # asks for the rest
orb gen job CleanupSessions --schedule "0 3 * * *" --timeout 5m --max-attempts 5 --yes
orb gen job SendDigest --every 6h --description "Emails the daily digest." --disabled --yes
orb gen job RebuildIndex --on-demand --dry-run
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
| `internal/app/jobs.go` | One `defineCleanupSessionsJob(defs, deps)` line after `//orb:anchor jobs` |

The job name is public API: record it with `go test ./internal/app -run TestPublicSurface -update`, which updates `api/surface.json` ([stability](stability.md)).

Safety checks: the app must have `internal/app/jobs.go` with the anchor; existing files are never overwritten; a job name can be registered once; the git repository must have no uncommitted changes (so the generated diff is easy to review) unless you pass `--allow-dirty`; generated Go is checked with gofmt.

## `orb gen resource`

Generates a module for records that belong to the signed-in user, in an app created with the Full preset: domain rules, use cases, a repository with hand-written SQL, `/v1/<names>` endpoints, tests and a migration. In a multi-tenant app (`orb new --tenancy multi`) records belong to an organisation instead: endpoints under `/v1/orgs/{orgId}/<names>`, every use case checks membership and a `<module>.<resource>.read` or `.write` permission with `orgs.RequireMember`, and the tests include non-members, roles without the permission and cross-organisation requests ([ADR-0048](../adr/0048-organisations-v0-4.md)). Everything it writes is your code to change ([ADR-0039](../adr/0039-resource-module-template.md)); `examples/full-single/internal/modules/projects` is exactly what it generates for the first example below, and `examples/full-multi/internal/modules/projects` what it generates there.

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
| (flag only) Who the records belong to | `--scope user\|org` | `org` in multi-tenant apps (`tenancy: multi` in `gorbital.yaml`), `user` otherwise; `org` needs the orgs module |

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

Then write the SQL under `-- +goose Up`, run `go run ./cmd/migrate` and `go test ./...` (tests migrate a fresh database with every file).

- **Write the SQL before migrating.** Goose records an empty migration as applied, so SQL added to it afterwards never runs.
- **Edit it only until it is released.** Locally, `docker compose down -v` resets a database that already ran it; once released, add a new migration instead.
- A statement that contains semicolons, such as a function body, goes between `-- +goose StatementBegin` and `-- +goose StatementEnd`.
- Don't write `+goose` in other comments: goose reads any comment line containing it as an annotation and rejects the file.

Safety checks: the app must have `db/migrations`; the name may only use letters, digits, hyphens and underscores (at most 60), so it can't reach a path or the SQL; an existing file is never overwritten; the git repository must be clean unless `--allow-dirty`.

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
| `internal/app/infra_mail.go` | Replaced with the provider's configuration and constructor |
| `internal/app/infra_mail_test.go` | Replaced with the provider's tests and the fixtures the rest of the app's tests use, so `go test ./...` passes with either provider |
| `.env.example` | The block between `# orb:begin mail` and `# orb:end mail` holds the provider's variables; the `# aps:` markers of apps generated before the rename are read too and rewritten as `# orb:` |
| `.env` | Updated if it exists, or created from `.env.example` when there are values to save; values already there are kept. It is always left with mode 0600: an existing `.env` that other users could read (as `cp .env.example .env` makes it) is narrowed, with a warning |
| `gorbital.yaml` | `mail: resend` or `mail: smtp` |
| `gorbital.lock` | The provider and the new hashes of the files above that `orb` tracks (apps created before v0.5 keep their lock as it is) |
| `go.mod` | Requires the provider module (with a `replace` to your gorbital checkout when the app uses one), then `go mod tidy` |

After confirming, it prints numbered next steps: where to get the Resend key and verify your domain (or which SMTP variables are left), how to set the sender with `PUT /ops/settings/mail.from_email`, and how to send a test email with `POST /ops/mail/test`. The sender name, address and reply-to are runtime settings, so they're never asked here.

Safety checks: the app must have `internal/app/mail.go` and the `.env.example` block; the git repository must be clean unless `--allow-dirty`; `.env` must be ignored by git before a secret is saved in it; secret values are never printed or included in `--json` output. Running it with the provider already in place changes nothing.

## `orb add orgs`

Turns a single-tenant Full app into a multi-tenant one, on branch `orb-add-orgs`: organisations with members, one role each, invitations and personal workspaces, and projects under `/v1/orgs/{orgId}/projects` ([ADR-0048](../adr/0048-organisations-v0-4.md), [ADR-0050](../adr/0050-upgrades-and-adding-features.md)).

```bash
orb add orgs --dry-run     # what would change, per file
orb add orgs               # apply on branch orb-add-orgs
```

It merges the multi-tenant app's files into yours the way `orb upgrade` merges a release: files you never edited are replaced, your edits are merged or shown as conflicts. Then it adds two migrations after your existing ones:

| Migration | What it does |
|---|---|
| `<version>_orgs.sql` | Creates `orgs`, `org_members` and `org_invitations` |
| `<version>_orgs_convert.sql` | Gives every account a personal workspace it owns (a deleted account's workspace is deleted too, purged 30 days after the account's deletion), then moves each project into its owner's workspace: `org_id` and `created_by` replace `owner_id`. The table is changed in place, so columns you added stay; this step is skipped if `projects` no longer has `owner_id` |

Without conflicts it updates `go.mod`, builds, regenerates `api/openapi.json`, records `api/surface.json` and commits `Add organisations`. Then run `go test ./...`, apply the migrations (`orb dev`, or `go run ./cmd/migrate` in each environment) and merge the branch. Set `orgs.invitation_url` before inviting people.

Resources you generated with `orb gen resource` stay owned by users and keep working; the command lists them. To move one to organisations, generate it again with `--scope org` and move its data.

Other flags: `--json`, `--skip-tidy`, `--skip-build`. Safety checks: the app must be in git with no uncommitted changes, and on this release (run `orb upgrade` first). An app that already has organisations is left alone.

## `orb add rls`

Turns on row-level security in a multi-tenant app: a fifth isolation layer, in PostgreSQL, under the four organisations already have ([ADR-0061](../adr/0061-row-level-security.md), [guide](row-level-security.md)).

```bash
orb add rls --dry-run      # the files it would write
orb add rls                # write them in the working tree
go run ./cmd/migrate
```

| File | Change |
|---|---|
| `db/migrations/<version>_row_level_security.sql` | A copy of `db/row_level_security.sql`: forces row-level security, with the `org_isolation` policy, on every table with `org_id NOT NULL` except `org_members` and `org_invitations` |
| `gorbital.yaml` | `rls: true`, so `orb gen resource --scope org` adds the policy to new resources' migrations |
| `gorbital.lock` | `inputs.rls` and the new hash of `gorbital.yaml`, so `orb upgrade` keeps the line |

The app already sets the organisation on every database connection, so no code changes. It prints next steps: connect as a role that isn't a superuser and has no `BYPASSRLS` (PostgreSQL applies no policy to those), migrate, run `orb doctor` and the tests, and commit.

Other flags: `--json`. Safety checks: the app must be multi-tenant (run `orb add orgs` first), on this release (run `orb upgrade` first), and in git with no uncommitted changes. Running it again changes nothing.

## `orb upgrade`

Brings the files `orb` wrote into your app up to this release, on a branch, without losing your edits ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)).

```bash
orb upgrade --dry-run          # what would change, per file; writes nothing
orb upgrade                    # apply on branch orb-upgrade/<version>
orb upgrade --from v0.4.0      # apps created before v0.5 name the release that created them
```

It rebuilds every file exactly as the release recorded in `gorbital.lock` wrote it, checks each against the hash in the lock, and merges per file:

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

Without conflicts it then updates `go.mod` (new requirements and the new library version, then `go mod tidy`), runs `go build ./...`, regenerates `api/openapi.json` (with the Postman collection and `llms.txt`), records the app's public names in `api/surface.json` (`go test ./internal/app -run TestPublicSurface -update`; review what changed in the commit) and commits `Upgrade gorbital to <version>`. Run your tests (database tests need `orb dev` or `docker compose up -d --wait`) and merge the branch. With conflicts it exits with code 1, commits nothing, and lists the files to resolve and the commands to finish.

Where earlier releases come from: with an gorbital checkout (`--local`, the checkout your `go.mod` replaces the library with, or the one you run in), `git archive` of the release's tag or commit. Otherwise the `gorbital.dev/cli` module from the Go module proxy, verified by the checksum database: `orb upgrade` refuses when `GOSUMDB` is `off` or names a database other than `sum.golang.org`, or when `GONOSUMDB`, `GOPRIVATE` or `GOINSECURE` covers the module (patterns match as Go matches them, with or without a trailing slash). Templates are only rendered as text; nothing downloaded is run.

A checkout must be the top of its own git repository, outside the app's repository, and the release must be a commit of the gorbital repository (its `go.mod` is `module gorbital.dev`, or `apistock.dev` before the rename). `--local` pointing inside the app is refused. A detected checkout inside the app, such as a `replace` to a copy committed in the app, is passed over, so the release comes from the module proxy instead. Otherwise a commit to the app could supply both the earlier templates and the lock hashes that prove them. The name, module, preset, tenancy and mail provider read from `gorbital.lock` or `gorbital.yaml` are checked with the rules `orb new` uses before anything is rendered.

| Flag | Default |
|---|---|
| `--from` | the release in `gorbital.lock`. Needed for apps created before v0.5 and by development builds without a recorded commit |
| `--local` | detected, as above |
| `--dry-run`, `--json` | off |
| `--skip-tidy` | run `go mod tidy` |
| `--skip-build` | build, regenerate `api/openapi.json`, record `api/surface.json` and commit |

Safety checks: the app must be in git with no uncommitted changes, and the branch `orb-upgrade/<version>` must not exist yet.

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
  ok    orb            v0.5.0 built with go1.26.8
  ok    gorbital.yaml  full preset, single tenancy
  warn  docker         Docker isn't running or isn't installed
                       fix: start Docker Desktop (or Docker Engine with Compose v2): orb dev runs PostgreSQL and Mailpit in it
  ok    gorbital.lock  from orb v0.5.0; 3 of 214 files gorbital wrote are edited or removed
  ok    library        gorbital.dev v0.5.0
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
| `gorbital.yaml`, `gorbital.lock` | Either is unreadable, or the lock was written by a newer `orb` | The lock is missing, from before v0.5, or from an older `orb` (run `orb upgrade`) |
| `library` | A `replace` directive points at something that isn't an gorbital checkout | `go.mod` doesn't require `gorbital.dev` |
| `anchor` (Full preset) | A line generators insert after is gone: `//orb:anchor modules`, `//orb:anchor jobs`, `//orb:anchor user-permissions`, `//orb:anchor org-permissions` (multi-tenant), or the mail block in `.env.example` | |
| `.env` (Full preset) | It holds secrets and git doesn't ignore it | It's missing, git doesn't ignore it, or it lacks variables `.env.example` has |
| `api files` | | `api/openapi.json`, `postman_collection.json` or `llms.txt` doesn't match the code |
| `configuration`, `database` (Full preset) | The app's configuration doesn't load; the database ran migrations the code doesn't have | The database is unreachable, or migrations are pending |
| `row-level security` (Full preset) | | Row-level security is on and the database role is a superuser or has `BYPASSRLS`, a table's row-level security isn't forced, or an organisation table has no policy ([row-level security](row-level-security.md)) |

Values from `.env` are never printed. The database checks run the app's own `go run ./cmd/migrate --status --json`, so `orb` needs no database driver and reads the app's migration files. Exit code 1 when any check fails.

## `orb dev`

Builds and runs the app in the current directory, rebuilding when files change and loading `.env` (variables already set in the environment win).

In an app with a database (Full preset), before the first start it:

1. Creates `.env` from `.env.example` (mode 0600) when there is none, and fills an empty `AUTH_ENCRYPTION_KEYS` with a random development key ([ADR-0043](../adr/0043-two-factor-authentication.md)).
2. Checks Docker, and that the services' host ports are free: `POSTGRES_PORT`, `MAILPIT_SMTP_PORT` and `MAILPIT_WEB_PORT` (ports held by the app's own running services are fine). A taken port names the `.env` line that moves it.
3. Starts PostgreSQL and Mailpit from the app's `compose.yaml` with `docker compose up -d --wait`.
4. Applies migrations (`go run ./cmd/migrate`) and runs seed data (`go run ./cmd/seed`). The first run prints the administrator's password once ([ADR-0042](../adr/0042-development-seed-data.md)); later runs change nothing.
5. Prints the API, docs and email inbox addresses, then starts the app.

In every app whose `.env.example` declares `DEV_CONSOLE_TOKEN` and that runs with `APP_ENV=development`, `orb dev` also generates a random dev console token (256 bits) for the run, passes it to the app in its environment only (never to `.env` or any file), and prints the [dev console APIs](dev-console.md) address and the token. Rebuilds keep the token; the next run gets a new one. A `DEV_CONSOLE_TOKEN` already set in `orb dev`'s own environment is passed instead and not printed.

While it runs, a changed or new migration is applied before the restart; if it fails, the previous version keeps running. Services keep running after `orb dev` stops, so the next start is fast: `docker compose down` stops them, and `docker compose down -v` also deletes the database.

| Flag | Default |
|---|---|
| `--observability` | off. Also starts Grafana (`grafana/otel-lgtm`, the `observability` profile in `compose.yaml`) on `GRAFANA_PORT` (3000) and sets `OTEL_EXPORTER_OTLP_ENDPOINT` for the app, so its traces, metrics and logs appear there. Works in Minimal apps too. Grafana receives metrics over OTLP and doesn't scrape the app; to check the Prometheus endpoint locally, set `METRICS_ADDR=127.0.0.1:9464` in `.env` and open http://127.0.0.1:9464/metrics ([production](production.md#prometheus-metrics)) |
| `--no-services` | start services. Skips Docker and uses `DATABASE_URL` and `MAILPIT_SMTP_ADDR` from `.env` as they are; migrations and seed data still run |
| `--no-reload` | reload on change |
| `--interval` | 500ms between change checks |

Without Docker, a Full app stops with a message: install and start Docker, or point `DATABASE_URL` at an existing PostgreSQL and use `--no-services`. Without the CLI, the same steps are `cp .env.example .env` (and a key in `AUTH_ENCRYPTION_KEYS`), `docker compose up -d --wait`, `set -a; . ./.env; set +a` to export `.env`, `go run ./cmd/migrate`, `go run ./cmd/seed` and `go run ./cmd/api`.

The port check listens on `127.0.0.1` only. On macOS, a program listening on all addresses (such as another Docker project's PostgreSQL on `0.0.0.0:5432`) isn't detected, and `docker compose up` then fails with `Bind for 0.0.0.0:5432 failed: port is already allocated`; move the port the same way.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed (for example, the directory already exists) |
| 2 | Invalid usage: a bad flag or value, or a required value missing without a terminal |
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
| `orb add mail` | `provider`, `already_configured`, `files`, `env_variables`, `modules`, `dry_run` |
| `orb add rls` | `name`, `already_on`, `migration`, `files`, `dry_run` |
| `orb add orgs`, `orb upgrade` | `name`, `from`, `to`, `up_to_date`, `branch`, `changes` (`path`, `action`, `note`), `conflicts`, `unproven`, `committed`, `dry_run`, `user_scoped_modules` (`orb add orgs`) |
| `orb doctor` | `app`, `preset`, `tenancy`, `checks` (`name`, `status`, `detail`, `fix`), `failures`, `warnings` |
| `orb version` | `version`, `recipe`, `library` |

Commands, flags, exit codes and JSON fields are public API from CLI 1.0 ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)). Within `schemaVersion` 1, fields are only added; check the version before reading the rest. The shape of each output is recorded in `cli/internal/cli/testdata/json` and checked by `TestJSONOutputs` ([stability](stability.md), [ADR-0054](../adr/0054-api-freeze-and-scaffold-compatibility.md)).
