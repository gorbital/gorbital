# ADR-0054: API freeze: enforcing stability tiers and the scaffold compatibility promise

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0015, ADR-0016

## Context

v1.0 turns ADR-0015's stability tiers and ADR-0016's scaffold compatibility promise from text into checks that fail (roadmap v1.0, items 2 and 3). ADR-0015 listed the enforcement ("`gorelease` or `apidiff` on every pull request", "golden tests for `--json`", "CI fails on removal" of codes and actions) but none of it existed:

| Surface (ADR-0015 tier) | Today | Evidence |
|---|---|---|
| Stability markers | 28 of 29 public packages say `Stability: pre-1.0 (ADR-0015)`; `socialtest` says nothing; nothing checks them | `grep -rn 'Stability:'` |
| Exported Go API (stable) | 13 library modules (root and 12 under `modules/`); no record of the API, no check; `apidiff` and `gorelease` need published earlier versions, and every tag so far is v0 under the old `apistock.dev` module path | `git tag`, `go.mod` files |
| Error codes, audit actions, permissions and roles, setting keys, job names (stable, additive only) | Declared across the app and the library: `httpx.Mapping{Code: …}` and `httpx.NewProblem` literals, `Action:` literals, `Action…` constants and `userEvent("…")` helpers, two permission catalogs, a settings registry and job definitions; neither the registry nor the definitions can list their names | `internal/app/*.go`, `internal/modules/*/usecase`, `modules/settings`, `modules/jobs` |
| `/ops/*` paths and response fields (stable from 1.0) | `api/openapi.json` is regenerated and checked for drift, so any change passes as long as the file is regenerated | `TestOpenAPIUpToDate`, CI drift step |
| `orb --json` (stable from CLI 1.0, "with `schemaVersion`") | Eight commands print JSON through two encoders; no `schemaVersion`; `orb version` has no `--json`; tests unmarshal a few fields | `cli/internal/cli/*.go` |
| Scaffold compatibility promise (from 1.0) | `TestUpgradeFromV040` checks upgrades, not old scaffolds against a new library; no v1 tag exists yet | `cli/internal/cli/upgrade_e2e_test.go` |
| Release checks | `release-cli.yml` builds and signs `orb`; library tags run nothing | `.github/workflows` |

Constraints: core keeps its dependency budget (ADR-0019), so tooling that needs `golang.org/x/tools` lives in its own module; generated apps own their code and receive changes through `orb upgrade` (ADR-0050); a check must say what to run to fix it.

## Options

| Question | Option | Verdict |
|---|---|---|
| Exported API | `apidiff`/`gorelease` against the previous tag only | Rejected as the only check: needs a published earlier version (none under `gorbital.dev` yet), so it can't run on pull requests before 1.0, and it leaves no reviewable record |
| | **Committed text listings per module, in the style of Go's `api/go1.txt`, checked on every pull request; `gorelease` at release against the previous tag** | **Chosen**: a diff a reviewer reads; removals and signature changes are line removals; `gorelease` adds the semantic judgement (and the version number check) once tags exist |
| Listing tool | A script over `go doc` | Rejected: text output isn't stable enough to diff |
| | **A small Go program on `go/types` via `golang.org/x/tools/go/packages`, in its own module `internal/tools/apicheck`** | **Chosen**: exact types, generics and method sets; outside core's budget |
| App surface: error codes and audit actions | Run the app and record what it emits | Rejected: only covers exercised paths |
| | A hand-kept list the test compares with a grep | Rejected: two lists to update, and a grep can't tell an action from any dotted string |
| | **Scan the Go syntax of the app and every `gorbital.dev` package it links for the forms codes and actions are written in** | **Chosen**: nothing to maintain, and the forms are already uniform (every current code and action is found) |
| App surface: permissions, roles, settings, jobs | **Read the real declarations** (`declarePermissions`, `declareSettings`, `defineJobs`) | **Chosen**; needs `settings.Registry.Keys` and `jobs.Definitions.Names` (additions) |
| `/ops` contract | Keep checking drift of `api/openapi.json` | Rejected: regenerating the file hides a removal |
| | **A frozen baseline document and a structural comparison (`openapi.CheckCompatible`)** | **Chosen**; exported so apps can hold their own `/v1` API to the same rule |
| `--json` versioning | A `schemaVersion` per command | Rejected: scripts would need a table; one version for all output is simpler |
| | **One `schemaVersion: 1`, first in every object, with golden files of each command's shape** | **Chosen** |
| Scaffold compatibility | Replay a matrix of old apps with edits | Deferred: ADR-0016's release-time upgrade tests already cover edited apps; the promise is about unedited scaffold compiling and passing |
| | **Build `orb` at the latest `v1.*` tag, generate Full single and multi apps against this checkout, build, vet and test them** | **Chosen** |

## Decision

### 1. Stability markers

Every public package of the library modules states `Stability: stable` or `Stability: experimental` in its package doc; `internal/archtest.TestPackagesDeclareStability` walks the root module and `modules/` (skipping `internal/`, `testdata/`, commands, `cli`, `examples`, `spikes`) and fails for a package without one.

| Packages | Marker | Why |
|---|---|---|
| Core: `actor`, `app`, `audit`, `buildinfo`, `config`, `health`, `httpx`, `mail`, `page`, `ratelimit`, `requestid` | stable | Scaffold code imports all of them; the compatibility promise can't hold otherwise |
| Modules: `auditpg`, `auth`, `auth/passkey`, `auth/social`, `jobs`, `mail/resend`, `mail/smtp`, `openapi`, `orgs`, `postgres`, `ratelimitpg`, `releases`, `settings`, `telemetry` | stable | Same; `ratelimitpg` is new (ADR-0052) but already wired into every Full app |
| `openapi/reference` | stable, with a note | Its Go API is stable; the HTML, CSS and scripts it renders are not API (like email templates in ADR-0015) |
| `postgres/pgtest`, `auth/passkey/passkeytest`, `auth/social/socialtest` | stable, for tests only | Generated apps' tests call them, so their API follows the promise; what they do inside a test (template databases, key sizes) may change |

Nothing is experimental today. ADR-0015's `gorbital.dev/x/…` module stays the place for experimental packages; the marker exists so one can't be added silently.

### 2. API listings: `api/*.txt` and `apicheck`

- `internal/tools/apicheck` (own `go.mod`, depends on `golang.org/x/tools`) loads each library module's public packages as `GOOS=linux` and writes `api/gorbital.dev.txt`, `api/modules-auth.txt`, `api/modules-mail-resend.txt`, … One sorted line per identifier:

```text
pkg gorbital.dev/ratelimit, func Per(int, time.Duration) Limit
pkg gorbital.dev/ratelimit, method (*Limiter) Take(context.Context, string) (Decision, error)
pkg gorbital.dev/ratelimit, type Decision struct, Allowed bool
pkg gorbital.dev/app, type Option interface { apply }
pkg gorbital.dev/modules/jobs, const MinTimeout = 1000000000
pkg gorbital.dev/modules/jobs, const MinTimeout time.Duration
```

| Rule | Why |
|---|---|
| Parameters by type only, also inside function types | Renaming a parameter isn't a change |
| An interface's line names all its methods, exported or not | Adding a method breaks implementations: the line changes |
| Constants list type and exact value | Changing a value can break stored data or clients |
| Methods come from method sets (promoted ones included), on `T` or `*T` | Callers see promoted methods |
| Embedded fields are listed as `embedded T`; promoted fields aren't repeated | The embedded type has its own lines |

- `go run -C internal/tools/apicheck .` compares and fails on a **missing line** ("removed or changed (breaking)") and on a **new line** ("new, not recorded"), like Go's `api/next`. `-write` records the current API. CI runs the check in the `generated` job; the tool is tested with a fixture repository and is in the test, lint and govulncheck lists.

### 3. Public-surface inventory: `api/surface.json` in each Full app

`internal/app/surface_test.go` (the same file in both Full apps) builds the inventory and compares it with `api/surface.json`:

| Field | Collected from |
|---|---|
| `error_codes` | Every `httpx.DefaultCode` for 400–599 (Huma's validation and client errors), plus the Go syntax of the app's packages and of every `gorbital.dev` package it links (`go list -deps`): `httpx.Mapping{Code: "…"}` and `httpx.NewProblem(status, "…", detail)` |
| `audit_actions` | The same files: an `Action: "…"` field in any composite literal, constants named `Action…`, and literal arguments to a function or method of the same package whose parameter is named `action` (`userEvent("auth.login.failed", …)`, `m.control(ctx, id, "jobs.run.retried", …)`); only values matching the audit action pattern |
| `permissions`, `roles` | `permissionCatalogs()` in `permissions.go`: `platform` in both apps, `org` in multi-tenant apps |
| `settings` | `declareSettings` on a fresh registry, `settings.Registry.Keys` (new) |
| `jobs` | `defineJobs` with zero dependencies, `jobs.Definitions.Names` (new), plus `jobs.MailKind` |

- A recorded name that's gone fails ("public API: restore it, or remove it deliberately with -update and call it out as a breaking change"); a new name fails until recorded with `go test ./internal/app -run TestPublicSurface -update`.
- The file lives in the golden apps, so **generated apps get the check**: their own resources, jobs and settings become public API the moment clients use them, and the same test protects them. `orb gen resource` and `orb gen job` print the record step; the generator tests run it and check the generated codes and actions appear.
- **Upgrades re-record it.** `api/surface.json` joins the derived files `orb upgrade` and `orb add orgs` regenerate instead of merging; after building they run the record step, so the upgrade commit shows what changed. Removals from the library or templates are caught earlier, by the golden apps' own test.
- **Minimal apps have no inventory:** no database, audit log, settings, jobs or permissions, and three error codes from the library. They get one when a feature adds a catalog.

### 4. `/ops` baseline: `api/openapi.baseline.json` and `openapi.CheckCompatible`

- The golden Full apps commit `api/openapi.baseline.json`, a copy of `api/openapi.json` as of 1.0. `TestOpsAPICompatible` exports the current document and fails for every incompatibility under `/ops/`.
- `openapi.CheckCompatible(baseline, current []byte, prefix string) ([]Incompatibility, error)` compares operations under a prefix through `$ref`, nested objects, array items and map values:

| Reported | Direction |
|---|---|
| Path or method removed (path parameter renames allowed) | — |
| Parameter removed; parameter, body or property newly required | Request |
| Request property or media type removed; accepted types narrowed; enum values removed | Request |
| Response status, media type or property removed; property no longer always present; response types widened (such as adding `null`) | Response |
| Authentication added to a public operation | — |

  Descriptions, examples, bounds and formats aren't compared; additions pass.
- The baseline is **never edited to pass the test.** At each release, after the check passes, the maintainer copies the released document over it, so additions made since become protected. Generated apps inherit it from templates; apps can add `"/v1/"` to the test's prefixes after recording their own baseline.

### 5. `orb --json`: `schemaVersion`

- Every `--json` output starts with `"schemaVersion": 1` (`JSONSchemaVersion`): `new`, `gen job`, `gen resource`, `gen migration`, `add mail`, `add orgs`, `upgrade`, `doctor`, and `orb version --json` (new). One writer adds it, so a command can't forget.
- `TestJSONOutputs` records each command's output in `cli/internal/cli/testdata/json/<command>.json`, normalised to its shape: key order kept, arrays cut to two elements, numbers other than `schemaVersion` zeroed, versions, timestamps and machine-dependent text replaced. A removed, renamed or retyped field changes the file; rewriting it is a reviewed breaking change that needs `schemaVersion: 2` and a new major version of orb.

### 6. `gorelease` at release

`.github/workflows/release-library.yml` runs on library tags (`v1.2.0`, `modules/auth/v1.2.0`) and by hand before tagging: it finds the module's previous release tag and runs `gorelease -base=<previous> -version=<tag>`, or `-base=none` for a first release or when the previous tag had another module path (every `apistock.dev` tag). It checks the version number against the API change as well as compatibility.

### 7. Scaffold compatibility check

`TestScaffoldCompatibility` (`ORB_COMPAT=1`, in `cli/internal/cli`) builds `orb` from a git worktree at the latest `v1.*` tag, creates Full single- and multi-tenant apps with it using `--local` pointing at this checkout (old scaffold, new library), and runs `go mod tidy`, `go build`, `go vet` and `go test` in each. Without a `v1.*` tag it skips and says why; `ORB_COMPAT_FROM=<tag>` checks another release. CI runs it in the `compatibility` job with PostgreSQL and Mailpit.

`orb upgrade --major` isn't built: it needs a v2 to upgrade to, and ADR-0016's bridge release. It arrives with the first v2 bridge release.

## Why

- Each surface gets the cheapest check that fails before users do: text diffs for Go API, a scan for names written in uniform forms, a structural comparison for HTTP, golden files for CLI output, a real build for old scaffolds.
- Committed listings make every API change visible in review, including additions, which are the only changes allowed.
- Failing on unrecorded additions keeps the records complete, so a later removal can't pass unnoticed.
- `gorelease` needs published versions; running it at release, with the listings on every pull request, covers both before and after the first public tag.

## Trade-offs

- Adding API is one more step: `apicheck -write`, `TestPublicSurface -update`, `TestJSONOutputs -update`. Every failure message names the command.
- The surface scan recognises three ways of writing an audit action and two of writing an error code. A code or action built another way (such as with `fmt.Sprintf`) isn't seen; the house style is literals and constants, and the ADR and test document it.
- `apicheck` lists the API as Linux builds it; platform-specific API would be missed (none exists).
- The listings don't judge compatibility semantically (an added method on a sealed interface shows as a changed line and needs a reviewer's call); `gorelease` does at release.
- The baseline check doesn't compare bounds (a `maxLength` lowered from 200 to 100 passes). Kept simple on purpose; add cases when one matters.
- Recording `api/surface.json` during upgrades means an upgrade commit may show removed names; review it like any other upgrade diff.
- Library modules can't run `gorelease` until `gorbital.dev` resolves (they require `gorbital.dev v0.0.0` through `replace`).

## Consequences

- Library additions: `settings.(*Registry).Keys`, `jobs.(*Definitions).Names`, `openapi.CheckCompatible` and `openapi.Incompatibility`. Every package doc says `Stability: stable`.
- New files: `api/*.txt` (13 listings, 1 215 lines), `internal/tools/apicheck`, `internal/archtest/stability_test.go`; in both Full apps `api/surface.json`, `api/openapi.baseline.json`, `internal/app/surface_test.go`, `internal/app/api_compat_test.go` and `permissionCatalogs` in `permissions.go`; `cli/internal/cli/{json.go,json_test.go,compat_e2e_test.go}` and `testdata/json`; `release-library.yml`; a `compatibility` CI job.
- `orb`: `--json` output gains `schemaVersion`; `orb version --json`; upgrades and `orb add orgs` record `api/surface.json`; generator next steps include recording the surface.
- ADR-0015's enforcement section is now implemented as described here; ADR-0016's promise is checked from the first v1 tag.
- Roadmap v1.0 item 5 (reference pages for error codes, audit actions, permissions, settings and jobs) can generate from `api/surface.json`.

## Implementation notes (2026-09-16)

| Check | Result |
|---|---|
| Stability markers | 29 packages; removing the line from `page` fails `TestPackagesDeclareStability` with the package named |
| API listings | 13 modules, 1 215 lines; `apicheck` passes on the tree; fixture tests: a changed result type, a removed field and a method added to an interface are reported as breaking, a new function as unrecorded, a renamed parameter not at all |
| Surface inventory | full-multi: 77 error codes, 56 audit actions, 11 platform and 7 org permissions, 2 platform and 3 org roles, 22 settings, 7 jobs; every action found by `grep` in the app and library is recorded. Editing the file (a fake action added, a real one renamed) fails with both messages. Generated `customers` and `notes` resources (user and org scope) fail until recorded, then pass with their codes, actions and permissions in the file |
| `/ops` baseline | Both Full apps pass against their baseline; a baseline with an extra `/ops` path and response property fails with five incompatibilities; `CheckCompatible` unit tests cover each rule, recursive schemas and a document Huma generated |
| `--json` | Nine golden files; each output has `schemaVersion` first |
| Scaffold compatibility | No `v1.*` tag: the check skips with its message. `ORB_COMPAT_FROM=v0.5.0` (informational, v0 may break): fails at the first step, `orb new --local`, because v0.5.0 predates the rename and refuses a checkout whose module isn't `apistock.dev`; its scaffold imports `apistock.dev/…`, so it can't compile against this library at all. Expected for v0 |
| `gorelease` | In a clean worktree of the branch before this change, `gorelease -base=none -version=v1.0.0` passes for the root module; `modules/openapi` can't load `gorbital.dev v0.0.0` until the module path resolves, as noted in the trade-offs |

Two details differ from the roadmap line: the recorded 1.0 OpenAPI document is the whole document (`api/openapi.baseline.json`), checked under `/ops/`, so apps can reuse it for `/v1/`; and the surface inventory fails on unrecorded additions as well as removals, like the Go API check.
