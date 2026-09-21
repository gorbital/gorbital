# Upgrading apps

gorbital writes your app's code once, then you own it. When a new release improves those files, `orb upgrade` merges the improvements into your edited code on a branch, and `orb add orgs` uses the same merge to make a single-tenant app multi-tenant. Decision: [ADR-0050](../adr/0050-upgrades-and-adding-features.md).

## Two layouts

`orb new` creates apps on `gorbital.Main` from v0.2 on: `cmd/api/main.go`, your modules in `internal/modules` and your migrations in `db/migrations`, with sign-in, `/ops` and the wiring in the library ([ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md)). Apps created by orb v0.1 have the v0.1 layout, with `internal/app`, `cmd/migrate` and `cmd/seed`.

`gorbital.lock` records the layout, and every command that merges templates uses the app's own layout's templates: a v0.1 app keeps receiving v0.1-layout fixes and never gets v0.2 files mixed into it. It builds against the v0.2 library unchanged. Moving it to the new layout is a separate, opt-in step, `orb upgrade --layout v0.2`; until then `orb upgrade` says so in its report.

## How it keeps your edits

`gorbital.lock`, committed with your app, records the release that created it, the answers the templates used (name, module, preset, tenancy, email provider) and a hash of every file gorbital wrote.

<div class="steps">

1. **Rebuild what gorbital wrote.** `orb upgrade` renders the recorded release's templates, from your gorbital checkout or the Go module proxy with checksum verification, and checks every file against its hash.
2. **Merge each file.** Files you never edited take the new version; edits elsewhere in a file merge; edits to the same lines become conflict markers. Nothing you wrote is dropped.
3. **Finish on a branch.** Without conflicts it updates `go.mod`, builds, regenerates `api/openapi.json` and commits to `orb-upgrade/<version>`. Run your tests and merge the branch.

</div>

> [!NOTE]
> Migrations are never merged: new ones are added with their released names, and yours stay as they are. Files `orb gen` created aren't tracked, so they are never touched.

## Upgrade

<div class="code-group">

```bash terminal
orb upgrade --dry-run
orb upgrade
```

```text output
upgrade shop-api from v0.1.0 to gorbital v0.1.1

  merged    internal/app/routes.go
  update    internal/modules/ops/delivery/system.go

  committed on branch orb-upgrade/v0.1.1

  next: go test ./...
        then merge orb-upgrade/v0.1.1
```

</div>

The output above is an example. `v0.1.0` is the first public release; an app created with a development build before it, whose `gorbital.lock` records no release or commit, names the gorbital commit that created it: `orb upgrade --from <commit>` (see [upgrade notes](../guides/upgrade-notes.md#before-v010-development-builds)). With conflicts, the command exits with code 1, commits nothing and lists the files to resolve and the commands to finish.

## Move to the v0.2 layout

Opt-in, and only when you want it: a v0.1 app keeps working on the v0.2 library, and `orb upgrade` keeps merging v0.1-layout fixes into it for as long as v0.1 apps are supported. What you get by moving is less code to own: sign-in, `/ops`, client flags, email events, organisations and the wiring come from the library, and your modules keep their own code.

<div class="code-group">

```bash terminal
orb upgrade                        # the app must be on this release's v0.1 templates first
orb upgrade --layout v0.2 --dry-run
orb upgrade --layout v0.2
```

```text output
move shop-api from the v0.1 layout to v0.2 (gorbital.Main)

  library    internal/modules/auth  146 generated files you never changed; gorbital.dev/gorbital/authhttp runs them now
  library    internal/modules/ops  38 generated files you never changed; gorbital.dev/gorbital/opshttp runs them now
  template   internal/modules/ping/  the example module, unchanged: written from the v0.2 templates …
  converted  internal/modules/projects/  the app's module: routes, errors and permissions in its Module value
             5 operations are gorbital routes now: projects-create: POST /v1/projects; …
             6 error mappings moved from internal/app/module_projects.go into Module.Errors
  library    internal/app, internal/jobs, cmd/migrate, cmd/seed  98 generated files you never changed; …
  library    db/migrations  19 copies of the library's migrations kept as they are; …

  Files:       32 written, 296 deleted
  Modules:     projects converted
  Report:      UPGRADE-v0.2.md
```

</div>

### Before and after

```text
v0.1                                    v0.2
cmd/api/main.go      → internal/app     cmd/api/main.go       gorbital.Main(options()...)
cmd/migrate, cmd/seed                   cmd/api/mail.go, storage.go
internal/app/        ~100 files         internal/modules/     your modules, and modules.gen.go
internal/jobs/                          db/migrations/        your migrations; the library declares its own
internal/modules/auth, ops, flags,      api/                  openapi.json, surface.json, …
                  mailevents, orgs
internal/modules/<yours>
db/migrations/       yours and copies of the library's
```

### What each report line means

| Line | What it means |
|---|---|
| `library` | Generated code you never changed. It is deleted, and the library runs it |
| `template` | Written from the v0.2 templates, such as an example module nobody changed |
| `converted` | One of your modules, converted where it is: its `huma.Register` calls become `gorbital.Get`/`Post`/… with route options, and the error mappings and permissions it had in `internal/app` move into its `Module` value |
| `kept` | A built-in module you changed. The library's module is copied into `internal/modules` as the app's own code, your change is moved into the copy, and `gorbital.lock` records where it came from ([The code in your repo](../guides/the-code-in-your-repo.md)) |
| `carried` | A change moved to where it belongs: middleware you added to `internal/app/routes.go` becomes `gorbital.WithStack(stack)` (or `gorbital.WithMiddleware`) in `main.go`, with the file it lives in moved to `cmd/api` |
| `manual` | A change orb can't make: it stops the move and says why. Make it yourself, or convert the rest with `--allow-manual`, which keeps those files in `_upgrade-v0.1/` (the go command ignores the directory) |
| `follow-up` | Something left for you that doesn't stop the move, such as an HTTP test of the old composition root |

### Changes you made, and the hooks that replace them

A change to generated sign-in code keeps working: the module becomes the app's own copy of `authhttp`, and the report names the [option or hook](../guides/configuring-sign-in.md) that could replace it — `BeforeLogin`, `AfterLogin`, `OnRegister`, `RegisterFields`, `PasswordPolicy`, `RequireMFA`, `WithoutRegistration`, `Brand`, `RouteMiddleware` or a [sign-in method of your own](../guides/adding-a-sign-in-method.md). Using one of those instead, before or after the move, gives the module back to the library, which keeps fixing it. A copy stops receiving library fixes; `orb doctor` says when the library's version changes.

### What doesn't change

Your database, your migration history (the copies of the library's migrations stay, and `gorbital.Migrate` reads an identical copy as the same migration, so there is nothing to apply), your endpoints and their schemas. The API document gains `x-gorbital-guards`, which says what a route requires; its only other change is the `/ops` instance example, which takes the app's own name. Check both with `git diff api/openapi.json`. `api/surface.json` and `api/openapi.baseline.json` are rewritten at the same time: the built-in modules' names belong to the library now, so the file records only the app's own ([upgrade notes](../guides/upgrade-notes.md#moving-a-v01-app-to-the-v02-layout)). Routes that needed a token now answer 401 before the request body is validated, where v0.1 validated first.

### Going back

Nothing is committed. `git restore . && git clean -fd` undoes the move, and `git revert <commit>` undoes it after you commit. The v0.1 layout keeps working: `orb upgrade` merges its templates as before.

## Add organisations to an existing app

<div class="code-group">

```bash terminal
orb add orgs --dry-run
orb add orgs
```

</div>

It merges the multi-tenant app's files into yours on branch `orb-add-orgs` and adds a conversion migration after your existing ones that gives every account a personal workspace it owns and moves each project into its owner's workspace. The `projects` table is changed in place, so columns you added stay. Modules you generated stay owned by users and keep working; the command lists them. Apply the migrations in each environment, run your tests and merge the branch. See [Organisations](organisations.md).

| Layout | Organisation tables | `main.go` |
|---|---|---|
| v0.2 (`gorbital.Main`) | The library's organisations module brings its migrations, under their released versions | Gains `gorbital.WithModules(orgshttp.Module(auth))` |
| v0.1 | A migration copied into `db/migrations` before the conversion, then the later organisation migrations | `internal/app` is merged instead |

> [!WARNING]
> In the v0.2 layout, the organisations module's migrations have versions from 2026-09-16 and 2026-09-18, older than the migrations a database already ran, and the migration tool refuses to apply older versions to a database migrated past them. `orb add orgs` works on a new database; for a development database, reset it (`docker compose down -v`). An existing production database of a v0.2 single-tenant app can't take organisations this way yet ([known gap](../adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-new-apps-on-the-v02-layout-2026-09-17)).

> [!WARNING]
> Run `orb upgrade` first when your app is on an older release: `orb add orgs` adds organisations to this release's files.

Full flags and merge rules: [CLI reference](../guides/cli.md).
