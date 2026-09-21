# ADR-0090: Composing presets

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0014, ADR-0050 · **Builds on:** ADR-0021, ADR-0083 · **Related:** ADR-0089, ADR-0091

## Context

`orb new` offers three shapes, and each shape is a directory of templates generated from a golden application. That worked while there were three. v0.2.2 gives the developer two independent choices — how much sign-in (ADR-0089) and what a tenant is (ADR-0088) — and nine of their combinations are legal. Extending the present design by one directory per shape would mean nine template trees and nine golden applications.

The two trees that exist already show the cost. `v0.2/full` and `v0.2/full-multi` share 62 of their paths; 26 of those are byte-identical and the other 36 differ, almost all of them files of the same demonstration `projects` module. The whole structural difference between a single-tenant and a multi-tenant Full app is one renamed migration and one extra file.

Facts checked on 2026-09-21 against `main` (`93040dff`, v0.2.1):

| Area | Today | Evidence |
|---|---|---|
| Shapes on offer | three: `minimal`/single, `full`/single, `full`/multi | `cli/internal/recipes/recipes.go`, `presets` |
| What a shape is | `Preset{Name, Tenancy, Recipe, dir, mainDir}` — `dir` names the v0.1 tree, `mainDir` the v0.2 one | same |
| Layouts and tenancies | `LayoutV01`, `LayoutV02`; `TenancySingle`, `TenancyMulti` | same |
| Library version generated apps require | `LibraryVersion = "v0.2.1"` | same |
| Flags of `orb new` | `--preset`, `--tenancy`, `--module`, `--local`, `--no-eject`, `--no-git`, `--start`/`--no-start`, `--skip-tidy`, `--json`, `--yes`, `--no-input`, `--plain` | `cli/internal/cli/new.go`, `runNew` |
| Template files | 1 024 under `cli/internal/recipes/`: `full` 357, `full-multi` 423, `minimal` 32, `v0.2` 127 (`v0.2/full` 63, `v0.2/full-multi` 64), the rest generators and machinery | `git ls-tree -r --name-only main cli/internal/recipes/` |
| Overlap of the two v0.2 trees | 62 shared paths: 26 byte-identical, 36 differing; plus `db/row_level_security.sql.tmpl` and one renamed migration | `diff` over `v0.2/full` and `v0.2/full-multi` |
| Conditional logic in them | none. No `⟦if⟧` and no `⟦range⟧` in either tree: branching is done by having two trees | `grep -rl "⟦if\|⟦range" cli/internal/recipes/v0.2/` |
| Conditional logic elsewhere | 19 of the 20 resource templates already branch | `grep -rl "⟦if\|⟦range" cli/internal/recipes/resource/` |
| Delimiters | `⟦` and `⟧`, with `missingkey=error` | `recipes.go`, `renderTree` |
| Where trees come from | `//go:generate go run ./gen` builds five trees from five golden applications | `recipes.go`; `cli/internal/recipes/gen/main.go:44-48` |
| Golden applications | `examples/minimal`, `examples/full-single`, `examples/full-multi`, `examples/v0.1/{full-single,full-multi}`; `examples/apps/*` is documentation | `examples/` |
| The generation gate | job `generated`: `go generate ./internal/recipes/`, then `git diff --exit-code` over `internal/recipes/{minimal,full,full-multi,v0.2}` and `../examples/{full-single,full-multi}` | `.github/workflows/ci.yml:150-155` |
| The contract gates | `api/openapi.json` regenerated and diffed for `examples/minimal`, `examples/full-single`, `examples/full-multi` and both v0.1 apps | `.github/workflows/ci.yml:164-184` |
| Byte equality test | `TestNewAppIsTheGoldenApp` | `cli/internal/cli/new_test.go:156` |

Two things follow. The byte comparison is the mechanism that keeps templates honest, and it is worth keeping. And duplicating a tree in order to change one guard call and one SQL column is not a design; it is the absence of one.

## Options

### How many template trees

| Option | Verdict |
|---|---|
| One tree per combination | Rejected: nine trees of about 64 files each, nine golden applications to hand-maintain and regenerate, and every library change landing in nine places. Unmaintainable |
| A base tree plus per-feature overlay trees | Rejected: it moves the problem rather than solving it. Overlay order becomes the hard part — which overlay wins when two write `main.go`, what a three-overlay app looks like, and how CI proves any of it |
| **One tree plus a manifest that says which files each feature needs** | **Chosen**: the tree is the union of every shape, the manifest is the only variable, and one golden application still generates it |

### How many byte-golden apps

| Option | Verdict |
|---|---|
| All nine | Rejected: nine applications to keep compiling, nine OpenAPI documents, nine `api/` directories in the repository. The cost is paid on every library change and buys nine copies of one guarantee |
| None; rely on build-and-test only | Rejected: template drift is exactly what the byte comparison catches. A template that renders valid Go which no longer matches what a human would write still compiles and still passes tests |
| **Three, plus a build-and-test matrix for the other six** | **Chosen**: golden files are for the shapes we document, the matrix is for the shapes we support |

### Where the feature-to-file mapping lives

| Option | Verdict |
|---|---|
| In the render code, as a switch over paths | Rejected: invisible. Nothing shows a reader which files a feature owns, and a stale branch is found only when someone creates that shape |
| In each template's own front matter | Rejected: Go templates have no front matter, so it would have to be a comment the renderer strips — and stripping it breaks the byte comparison against the golden application, which has no such comment |
| **One `manifest.yaml` at the root of the tree** | **Chosen**: one file to read, one file to review, and a path missing from it is a build failure |

## Decision

### 1. Profiles replace presets

```go
// A Profile is the application orb new creates: how much sign-in, and what
// a tenant is.
type Profile struct {
	Auth      string   // AuthNone, AuthBasic, AuthFull
	Scope     string   // ScopeNone, ScopeSingle, ScopeCustom, or a scope name
	ScopeName string   // "organisation", "merchant", … when Scope is a name
	Methods   []Method // sign-in methods; empty means the Auth default
}
```

`--auth none|basic|full` and `--scope none|single|custom|<name>` replace `--preset` for Full applications. `--tenancy single|multi` keeps working for all of v0.x as a deprecated alias: `single` maps to `--scope single`, `multi` to `--scope organisation`. `--preset minimal` is unchanged and keeps the v0.1 layout.

### 2. Nine legal combinations, enumerated in code

`recipes.LegalProfiles()` returns them. Anything else is refused by `orb new` with a message that says why, not merely that.

| `--auth` | `--scope` | What the application gets | Verified by |
|---|---|---|---|
| `none` | `none` | no sign-in, no tenants; routes are public or guarded by the application's own middleware | matrix |
| `basic` | `none` | password sign-in and operators; data owned by users, no tenant column | **`examples/api-basic`** (golden) |
| `basic` | `single` | password sign-in; one implicit scope, data owned by users | matrix |
| `basic` | `custom` | password sign-in; membership tables and a `ScopeAuthorizer` stub, no `orgshttp` | matrix |
| `basic` | `<name>` | password sign-in; `orgshttp` under the chosen vocabulary | matrix |
| `full` | `none` | every sign-in method; no tenants | matrix |
| `full` | `single` | every sign-in method; one implicit scope | **`examples/full-single`** (golden) |
| `full` | `custom` | every sign-in method; membership tables and a stub, no `orgshttp` | matrix |
| `full` | `<name>` | every sign-in method; `orgshttp` under the chosen vocabulary | **`examples/full-multi`** (golden, `organisation`) |

`--auth none` pairs only with `--scope none`. Data owned by users needs users, and a scope's membership is a relation between a subject and a tenant; with no authenticated subject there is nothing for the guard to check. The refusal says so:

```text
--auth none --scope single is not a shape orb can create.
A scope decides which user may act in it, and --auth none means the app has
no users. Use --scope none, or --auth basic.
```

### 3. One template tree

```text
cli/internal/recipes/
├── minimal/              32 files   unchanged (v0.1 layout)
├── full/                357 files   unchanged (v0.1 layout)
├── full-multi/          423 files   unchanged (v0.1 layout)
└── v0.2.2/                            replaces v0.2/full and v0.2/full-multi
    ├── manifest.yaml
    ├── cmd/api/main.go.tmpl
    └── …
```

`v0.2/full` and `v0.2/full-multi` are deleted. The tree is still generated, not written: `//go:generate go run ./gen` builds `v0.2.2/` from `examples/full-multi`, the richest shape, and the manifest decides what a narrower shape leaves out. The CI gate at `.github/workflows/ci.yml:150-155` keeps its form, with `internal/recipes/v0.2` replaced by `internal/recipes/v0.2.2`.

### 4. The manifest is the only mapping

```yaml
# cli/internal/recipes/v0.2.2/manifest.yaml
paths:
  cmd/api/main.go.tmpl:                       always
  db/row_level_security.sql.tmpl:             [scope.named]
  internal/modules/orgs/**:                   [scope.named]
  internal/modules/scope/authorizer.go.tmpl:  [scope.custom]
  internal/modules/auth/**:                   [auth.local]
  AUTH_PROVIDERS.md.tmpl:                     [auth.full]
```

`Render` writes a path when its features are on and skips it otherwise. A path present in the tree and absent from the manifest is an error naming the path, so a newly added template cannot be silently forgotten — the failure happens in `go generate` and in CI, not in a user's application. A manifest entry naming a path that does not exist is the same error the other way round.

### 5. The application's API artefacts are produced, not templated

Five of the files in the v0.2 trees are **outputs** of the application, not inputs to it: `api/openapi.json`, `api/openapi.baseline.json`, `api/postman_collection.json`, `api/surface.json` and `api/llms.txt`. Between `v0.2/full` and `v0.2/full-multi` they account for 7 089 of the 7 700 differing lines, and they differ for the only reason they could — the two applications expose different routes.

They stop being templates. After `go mod tidy`, `orb new` runs the application's own command, exactly as CI does for the golden applications (`.github/workflows/ci.yml:164-178`):

```bash
go run ./cmd/api openapi --dir api
```

`orb new` already writes `api/surface.json` this way after copying the built-in modules, so this extends a step that exists rather than adding a kind of step. `TestNewAppIsTheGoldenApp` is unaffected: the produced files must still equal the golden application's, byte for byte.

The cost is one compile and run at creation, on a tree that has just been built by `go mod tidy`. The first-run budget in [`docs/benchmarks.md`](../benchmarks.md) gains a line, and a regression beyond it fails the benchmark gate.

### 6. Conditional logic lives in exactly six templates

| Template | What it branches on |
|---|---|
| `cmd/api/main.go.tmpl` | which modules are constructed and in what order |
| `.env.example.tmpl` | which variables the chosen modules read |
| `compose.yaml.tmpl` | which services the chosen modules need |
| `README.md.tmpl` | what the application does and how to run it |
| `AGENTS.md.tmpl` | what an agent may and may not edit here |
| `gorbital.yaml.tmpl` | the recorded profile |

Every other template is substitution only. The distinction is between `⟦.Scope.Name⟧`, which is data, and `⟦if .Scope.Named⟧`, which is a second shape to read, test and keep true. The rule applies to the application tree. The resource generator's templates branch already, because a resource's access rule is their subject (ADR-0091), and the demonstration `projects` module comes from there rather than from the six.

That last point is what makes one tree possible at all. Measured on `v0.2.1`, the two v0.2 trees share 62 paths and **36 of them differ**; 25 of the 36 are the `projects` module, which differs only because the resource is owned by a user in one tree and by an organisation in the other — the distinction `orb gen resource --scope` exists to express. Generating the demonstration resource instead of templating it removes those 25, the five artefacts above remove five more, and the remaining six are exactly the conditional templates in the table.

### 7. Three byte-golden applications and a six-way matrix

`TestNewAppIsTheGoldenApp` keeps comparing `orb new` output byte-for-byte against `examples/full-multi`, `examples/full-single` and the new `examples/api-basic`. Those three are documented shapes: each has an `api/openapi.json` gate, appears in the guides, and is what a reader is shown.

The other six get a CI matrix that creates the application, builds it, vets it and runs its tests. Not byte equality — the six have no golden application to compare with, and inventing one would put us back at nine.

> Golden files are for the shapes we document. The matrix is for the shapes we support.

### 8. The profile is recorded, so nothing asks twice

`gorbital.yaml` gains `auth`, `scope` (`name`, `plural`, `param`, `table`, `members`, `column`), `roles` and `methods`. `gorbital.lock` records the same profile beside the existing `preset`, `tenancy` and `layout` inputs, so `orb upgrade` rebuilds the merge base from the templates the application was actually written from. `orb doctor` checks two things it could not check before: the manifest against the files on disk, and `cmd/api/main.go` against `gorbital.yaml`.

## Consequences

- The template count falls. `v0.2/full` (63 files) and `v0.2/full-multi` (64) become one tree of about 65 including the manifest — 127 files down to 65, in a directory of 1 024.
- Adding a feature means one manifest entry and, at most, one branch in one of the six. It does not mean a new tree, a new golden application or a new `api/` directory.
- A template added without a manifest entry fails `go generate` and fails CI. The failure mode that worries us — a file that silently stops being written for some shape — becomes impossible to introduce quietly.
- CI grows a nine-way matrix. The three golden shapes are covered by the existing byte comparison and OpenAPI gates; the six others each create, build, vet and test one application on their own runner. They run in parallel, so wall-clock grows by roughly one application's create-build-test — the existing end-to-end step budgets 40 minutes for a run over two Full shapes — while runner-minutes for new-application testing rise about four and a half times.
- The `v0.1` trees (`minimal`, `full`, `full-multi`, 812 files) are untouched. v0.1 applications still regenerate from them through `orb upgrade`, and their five golden applications and OpenAPI gates stay exactly as they are. This decision is about the v0.2 layout only.
- `Preset` and `LookupPreset` go from the public shape of `orb new` to internal machinery for the Minimal and v0.1 paths. `gorbital.lock` files written by v0.2.1 keep validating, because `preset` and `tenancy` are still read.
