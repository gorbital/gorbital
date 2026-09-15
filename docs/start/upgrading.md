# Upgrading apps

gorbital writes your app's code once, then you own it. When a new release improves those files, `orb upgrade` merges the improvements into your edited code on a branch, and `orb add orgs` uses the same merge to make a single-tenant app multi-tenant. Decision: [ADR-0050](../adr/0050-upgrades-and-adding-features.md).

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
upgrade shop-api from v0.4.0 to gorbital v0.5.0

  merged    internal/app/routes.go
  update    internal/modules/ops/delivery/system.go
  create    db/migrations/20260915000006_auth_social.sql

  committed on branch orb-upgrade/v0.5.0

  next: go test ./...
        then merge orb-upgrade/v0.5.0
```

</div>

Apps created before v0.5 name the release that created them: `orb upgrade --from v0.4.0`. With conflicts, the command exits with code 1, commits nothing and lists the files to resolve and the commands to finish.

## Add organisations to an existing app

<div class="code-group">

```bash terminal
orb add orgs --dry-run
orb add orgs
```

</div>

It merges the multi-tenant app's files into yours on branch `orb-add-orgs` and adds two migrations after your existing ones: the organisation tables, then a conversion that gives every account a personal workspace it owns and moves each project into its owner's workspace. The `projects` table is changed in place, so columns you added stay. Resources you generated with `orb gen resource` stay owned by users and keep working; the command lists them. Apply the migrations in each environment, run your tests and merge the branch. See [Organisations](organisations.md).

> [!WARNING]
> Run `orb upgrade` first when your app is on an older release: `orb add orgs` adds organisations to this release's files.

Full flags and merge rules: [CLI reference](../guides/cli.md).
