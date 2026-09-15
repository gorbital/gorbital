# ADR-0050: Upgrading apps and adding features to them (v0.5)

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0014, ADR-0016, ADR-0021, ADR-0041, ADR-0048

## Context

v0.5 promises `aps upgrade` (a 3-way merge on a branch), `aps add orgs` (single-tenant to multi-tenant) and the Custom preset. Its definition of done: an app generated with an earlier release and edited by script upgrades with no lost edits.

ADR-0016 and ADR-0021 planned this on top of per-feature recipes: a fixed set of operations (`createFile`, `insertLine@anchor`, `addRequire`, `copyMigration`, `appendEnv`), recorded in `apistock.lock` and replayed at the old and new versions to rebuild the merge base. What shipped in v0.2 to v0.4 is different, for good reasons (ADR-0041):

| Finding | Evidence | Consequence |
|---|---|---|
| A preset is one whole template tree, generated from a golden app | `cli/internal/recipes/{minimal,full,full-multi}`, byte-for-byte golden tests | There are no per-feature recipes to replay; the tested unit is the whole app |
| The lock can't identify the templates that wrote a file | `apistock.lock` records `generator: aps v0.1.0-dev` and recipe version `v0.1.0` (the constant `LibraryVersion`, never bumped); no commit, no template inputs | An upgrade can't know what the old templates were |
| Organisations change shared files wholesale, not at anchors | `internal/archtest/examples_test.go` lists `app.go`, `commands.go`, `jobs.go`, `modules.go`, `permissions.go`, `settings.go`, `seed.go` among the files `full-multi` changes | "One line at one anchor per feature" doesn't describe organisations |
| Multi-tenant apps replace a migration rather than add one | `full-single` has `20260915000002_projects.sql` (`owner_id`); `full-multi` has `20260916000002_projects.sql` (`org_id`) instead | Moving a live app to organisations can't copy `full-multi`'s migrations: released migrations never change (ADR-0016), and `CREATE TABLE projects` would fail on an existing database |
| `aps add mail` rewrites tracked files but doesn't record it | `cli/internal/cli/add.go` never touches `apistock.lock` | After `aps add mail`, a rebuilt base would still say Resend |
| Nothing is released | No tags; milestones merged at `c830679` (v0.2), `83ae970` (v0.3), `0004f6c` (v0.4) | "Generated with v0.2" has no release to name yet |

The merge spike (`spikes/merge`) holds: merges keep every edit, edits on neighbouring lines conflict, and a conflicted file doesn't compile, so upgrades run on a branch.

Constraints: developer code is never overwritten; generated apps work without `aps` (architecture principle 8); no code from outside the CLI runs during an upgrade and nothing is written outside the project (threats 2 to 4); the golden apps stay the single source of truth.

## Options

### Where the merge base comes from

| | 1. Embed every release's templates in `aps` | 2. Fetch the recorded release's templates | 3. Keep a pristine copy in the app's git repository |
|---|---|---|---|
| Shape | Each release adds its trees to the binary | `go mod download apistock.dev/cli@<version>`, verified by the checksum database, rendered by the current `aps` | A branch or ref holding exactly what `aps` wrote, committed at `aps new` and every `aps add` |
| Binary size | Grows about 0.8 MB per release, forever | Unchanged | Unchanged |
| Works offline | Yes | After the first download (module cache) | Yes |
| Teammates and CI | Work | Work | Need the ref fetched and pushed, which git doesn't do by default for non-branch refs; a visible branch gets deleted or rebased |
| Trust | Our binary | Our release, checked against the checksum database; templates are rendered as text, never run | Whatever is in the repository |
| Survives Go or gofmt changes | Verified by hashes | Verified by hashes | Exact |

### What a recipe is

| | A. Per-feature operations (ADR-0021 as written) | B. Whole preset trees merged 3-way |
|---|---|---|
| Shape | Split golden apps into feature fragments and anchor edits; replay the log | A tracked state is a preset tree plus its inputs; upgrading or adding a feature merges old tree → new tree into the app |
| Fits what shipped | No: rewrite the generator and golden tests | Yes: the golden trees are the recipes |
| Organisations | Needs dozens of anchors in shared files | Merge `base-full` → `base-full-multi` |
| Conflicts | Fewer in shared files | More in shared files a developer edited; always on a branch, never lost |
| Custom preset | Arbitrary combinations, each needing tests | Only combinations with a golden tree |

## Decision

Option 2 for the base, option B for recipes.

### 1. The lock records what's needed to rebuild the base

`apistock.lock` moves to `apiVersion: apistock.dev/v2`:

```json
{
  "apiVersion": "apistock.dev/v2",
  "aps": { "version": "v0.5.0", "revision": "0004f6c…" },
  "inputs": { "name": "acme-api", "module": "example.com/acme-api", "preset": "full", "tenancy": "single", "mail": "resend" },
  "files": [ { "path": "internal/app/app.go", "sha256": "…" } ]
}
```

- `aps` is the release that rendered the tracked files; `revision` is the commit, read from the binary's build information, for development builds.
- `inputs` are every value the templates read, apart from the machine-specific `--local` path.
- `files` holds the hash of every tracked file exactly as `aps` wrote it. `aps add mail`, `aps add orgs` and `aps upgrade` rewrite the lock; `aps gen resource`, `aps gen job` and `aps gen migration` stay one-shot and untracked (ADR-0021).
- `apistock.dev/v1` locks are read too: they have no version to rebuild from, so `aps upgrade --from <tag or commit>` names it, and the rebuild must match every recorded hash or the upgrade stops.
- `go.mod` and `go.sum` are rendered but never hashed: `aps new` runs `go mod tidy` straight after writing them, and upgrades update them with `go get` (section 3).
- Recipe names (`base-full`, `base-full-multi`) are no longer recorded: `inputs.preset` and `inputs.tenancy` select the tree.

### 2. Rebuilding the base

1. Get the recorded release's recipe trees: from the module cache or the Go module proxy (`go mod download`, checked by the checksum database; refused when `GOSUMDB=off`, or `GONOSUMDB`, `GOPRIVATE` or `GOINSECURE` covers `apistock.dev`); for a development build, `git archive <revision> cli/internal/recipes` from the `--local` checkout.
2. Render them with the recorded inputs using the current `aps` renderer. Fetched code is never compiled or run; templates are data. The template format (`⟦ ⟧` delimiters, the `recipes.Data` fields) becomes a versioned contract: fields are only added.
3. Compare each rendered file with its lock hash. A file that differs (for example gofmt changed between Go releases) has no trustworthy base: the upgrade treats it as changed on both sides, so the developer sees both versions in conflict markers instead of losing either.

### 3. Merge rules

`base` is the rebuilt old tree, `theirs` the new tree, `ours` the working copy.

| Case | Result |
|---|---|
| `ours` = `base` | Take `theirs` |
| `base` = `theirs` | Keep `ours` |
| Both changed | `git merge-file` with labelled markers (`yours` / `apistock vX`) |
| New in `theirs` | Created; if a file already exists at that path, conflict |
| Removed in `theirs` | Deleted if `ours` = `base`, otherwise kept and reported |
| Deleted by the developer | Stays deleted; reported if `theirs` changed it |
| `go.mod`, `go.sum` | Never merged: `go get` each `apistock.dev` module at the new version plus requirements the new tree added, then `go mod tidy` |
| `db/migrations/*.sql` | Never merged: new migration files are copied with their released names; a release that changes a released migration fails CI |
| `api/openapi.json` | Never merged: regenerated with `go run ./cmd/api openapi` once the app builds |
| `apistock.yaml`, `apistock.lock` | Rewritten from the new inputs and hashes |

Hashes, not timestamps, decide "unchanged", so line-ending or permission changes don't count as edits.

### 4. `aps upgrade`

- Needs a git repository with a clean tree; creates branch `aps-upgrade/<version>` from `HEAD`.
- Applies the merge, then `go.mod` and migrations. Without conflicts: `go build ./...`, `go vet ./...`, regenerate `api/openapi.json`, and commit `Upgrade apistock to <version>`; tests are left to the developer, since they need the database, and the next steps say so.
- With conflicts: nothing is committed; it lists each conflicted file and prints the commands to finish (resolve, `go build ./...`, `go run ./cmd/api openapi > api/openapi.json`, commit).
- `--dry-run` prints the plan per file; `--json` for scripts and agents; releases can be skipped, because the base is rebuilt from whatever release the lock names.
- The upgrade notes of every release between the two are printed, so manual steps are visible.

### 5. `aps add orgs`

- The same merge, with `base` = the app's `base-full` tree and `theirs` = the `base-full-multi` tree at the same release. An app on an older release runs `aps upgrade` first; `aps add orgs` refuses otherwise.
- Migrations can't come from `full-multi`: the recipe adds two new migrations stamped at the time of the command, like `aps gen migration`. The first is `full-multi`'s organisations migration. The second converts the data: a personal workspace for every existing account, `projects.org_id` set to the owner's workspace, `owner_id` becoming `created_by`, and indexes replaced. The app's released `projects` migration stays.
- A CI test proves the result: a database migrated as single-tenant with data, then converted, has the same schema (`pg_dump --schema-only`) as a new multi-tenant app, every project sits in its owner's workspace, and the converted app's tests pass.
- Resources the developer generated with `--scope user` are left as they are and listed: user-scoped resources are valid in multi-tenant apps. Converting one to organisations is a manual step the output explains.
- Recorded in the lock: `inputs.tenancy` becomes `multi`.

### 6. `aps add mail` records its change

It sets `inputs.mail` and the hashes of the files it rewrote, so the next upgrade rebuilds an SMTP base for an SMTP app.

### 7. ADR-0021's operation vocabulary is dropped

The five-operation vocabulary and the replayed operation log are replaced by rendered trees, recorded inputs and hashes. What stays: writes through `os.Root`, gofmt validation, idempotence, a clean tree, `--dry-run` and `--json`, anchors for `aps gen resource`, and no code run at install time. "One wiring file per feature" stays a style goal for the golden apps: it keeps merges small, but correctness no longer depends on it.

### 8. The Custom preset leaves v0.5

With whole trees, every Custom combination is another golden app to write and test. The only real choices in a Full app today are tenancy (asked) and the email provider (`aps add mail`); sign-in methods are off until their keys are set (ADR-0045). A checklist would mainly remove things that are cheap to leave in and hard to add back. Custom returns only when a feature is genuinely optional and costs something to ship, with its own ADR and a golden tree per offered combination.

### 9. Order of work

1. Lock v2 in `aps new` and `aps add mail`.
2. Base rebuild and the merge engine, with a test that upgrades a scripted-edit app across a real template change.
3. `aps upgrade`.
4. `aps add orgs` with the conversion migration and schema test.
5. The rest of v0.5 (ops endpoints, maintenance mode, `aps doctor`, Postman collection, `llms.txt`) in their own ADR. Those changes touch the golden apps, so they are the first real upgrade CI exercises.

### 10. Definition of done, restated

Before the first release, "generated with v0.2" means: tag `v0.2.0`, `v0.3.0` and `v0.4.0` at their milestone commits (with `cli/`-prefixed tags too, since the CLI is a nested module); CI generates a Full app with the CLI at `v0.4.0`, applies scripted edits (a changed line in a tracked file, a new resource, a new migration, `aps add mail smtp`), runs `aps upgrade --from v0.4.0` with the v0.5 CLI, and checks that every scripted edit survives, the template changes arrive, and the app builds and passes its tests. The same app converted with `aps add orgs` passes the multi-tenant suite.

## Why

- The golden apps stay the only source of truth: the tree a developer reads in this repository is both what `aps new` writes and what `aps upgrade` merges.
- Hashes make the base provable instead of assumed; a base that can't be proven becomes a visible conflict, never a silent overwrite.
- Fetching through the module proxy and checksum database reuses how Go developers already trust code, keeps the binary small and works for teammates and CI.
- One merge engine serves upgrades and organisations; `aps add orgs` isn't a separate patcher.

## Trade-offs

- More conflicts in shared wiring files a developer has edited than anchor edits would give; they happen on a branch with both sides shown.
- Upgrades need git and, the first time, network access to the module proxy.
- The template format is now a contract across releases.
- Dropping the operation vocabulary means third-party recipes (ADR-0021's "safe to run from third parties") need a new design when community modules arrive.
- Custom is later, not v0.5.

## Consequences

- ADR-0016: the base is rebuilt from recorded inputs and hashes, not replayed operations.
- ADR-0021: sections "Engine" steps 1 and 7, "Operations" and the lock requirement are superseded by sections 1, 3 and 7 here.
- ADR-0014 and roadmap: Custom leaves v0.5.
- ADR-0015: `apistock.lock` v2 and the template format are versioned public contracts; the CLI reads v1 and v2.
- ADR-0048: `aps add orgs` converts data with a new migration instead of copying `full-multi`'s.
- Threat model: new rows for tampered fetched recipes (checksum database, templates never run) and upgrades overwriting edits (hash-proven base, branch, conflicts).
- Every template change to a golden app needs an upgrade note; CI fails when a released migration changes.

## Implementation notes

| Step | State | Notes |
|---|---|---|
| 1. Lock v2 | Done (2026-09-15) | `cli/internal/cli/lock.go`. `aps new` writes v2; `aps add mail` sets `inputs.mail` and rehashes the tracked files it rewrote when the lock is v2, and leaves a v1 lock alone (`apistock.yaml` holds the provider for those apps). `revision` is recorded only when the binary was built from a clean tree, since a modified tree's templates aren't that commit. Reading refuses unknown fields, unknown versions, and paths that are absolute, escape the app or repeat. Tests: `TestLockRoundTrip`, `TestReadLockV1`, `TestReadLockRejects`, `TestLockRecordOnlyTrackedFiles`, `TestRevisionOf`, `TestNewCreatesApp` (every hash matches the written file), `TestAddMailRecordsTheProviderInTheLock` |
| 2. Base rebuild and merge engine | In progress | Rendering done (2026-09-15): `recipes.Release` renders a whole app in memory from any release's `cli/internal/recipes` directory (`Embedded()` or `ReleaseFS`), preset tree then email provider, exactly as `aps new` and `aps add mail` write it; `aps new` now renders the same tree and writes it. The layout and template format are unchanged since `v0.2.0`, so every tagged release renders with the current code. Tests: `TestTreeMatchesRender`, `TestTreeFromDirectory`, `TestTreeWithSMTP`, `TestTreeErrors`, and every lock written by `aps new` or `aps add mail` rebuilds hash for hash (`assertLockRebuilds`). Next: fetching a release's templates (checkout `git archive`, module proxy) and the 3-way merge |
| 3. `aps upgrade` | | |
| 4. `aps add orgs` | | |

## Maintainer's answers (2026-09-15)

1. The base is rebuilt from the recorded release through the Go module proxy (section 2), not kept as a pristine copy in git or embedded in `aps`.
2. Whole preset trees merged 3-way replace ADR-0021's per-feature operations (sections 3 and 7).
3. The Custom preset moves out of v0.5 (section 8).
4. `v0.2.0`, `v0.3.0` and `v0.4.0` are tagged at the milestone commits, with `cli/` tags, and pushed (section 10).
