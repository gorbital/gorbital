# ADR-0093: The examples repository

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0084 · **Builds on:** ADR-0083 · **Related:** ADR-0090, ADR-0092

## Context

`examples/` holds two kinds of thing that look alike from the directory listing and are not alike at all.

The **golden apps** — `examples/minimal`, `examples/full-single`, `examples/full-multi`, `examples/v0.1/full-single` and `examples/v0.1/full-multi` — are the source the CLI's templates are generated from. `go generate ./cli/internal/recipes/` renders `cli/internal/recipes/*` out of them, and CI byte-compares both the rendered templates and the golden apps themselves. They are not documentation; they are input to the build, and the build fails if they move.

The **showcase apps** — the six directories under `examples/apps/` — look like documentation. Their job is to be included, by marker, into pages a reader reads. They are 1 637 files, and the library's CI builds and tests all six on every pull request.

Five of them are documentation and nothing else. **`shelfie` is not**, and this record said it was. The `cli` module's tests read it: `cli/internal/recipes/module_test.go` compares `orb gen module`'s rendered output with `shelfie/internal/modules/shelves` and `internal/modules/clubbooks` file by file, and `cli/internal/cli`'s tests copy the whole application to run `orb gen module`, `orb eject` and `orb doctor` against a real one. It is a fixture in the same category as the golden apps, and it stays — at `examples/shelfie`, next to them, not under an `examples/apps/` that is going away.

The mistake was easy to make and worth naming, because the check that would have caught it is not the obvious one: the `cli` module is a separate Go module, so `go test ./...` from the repository root never runs those tests.

This record moves the documentation-only apps out and keeps everything the build reads. The distinction is the whole decision; a reader who takes "the examples move" to mean all of `examples/` has misread it.

Facts checked on 2026-09-21 against `main` (`93040dff`, v0.2.1):

| Area | Today | Evidence |
|---|---|---|
| Golden apps | Five: `minimal`, `full-single`, `full-multi`, `v0.1/full-single`, `v0.1/full-multi`, each the hand-written source of a preset's templates | package doc of `cli/internal/recipes/recipes.go` |
| Golden fixture | `shelfie`, the golden output `orb gen module` is compared against, and the application four of the CLI's test files copy | `cli/internal/recipes/module_test.go:12`, `cli/internal/cli/gen_module_test.go`, `eject_test.go`, `json_test.go` |
| Why they cannot move | `go generate ./internal/recipes/` then `git diff --exit-code` over `internal/recipes/…` **and** `../examples/full-single ../examples/full-multi` | `.github/workflows/ci.yml:150`–`155` |
| Showcase apps | Six under `examples/apps/`: `plateful` 525 files, `shelfie` 453, `invoicing` 326, `admin-tool` 256, `mobile-backend` 41, `payments` 36 — 1 637, plus the directory's README. Five move; `shelfie` is a fixture and stays | `git ls-tree -r --name-only main examples/apps/<name>` |
| Their CI cost | One job builds, vets, gofmt-checks and `go test -race`s every `examples/apps/*/go.mod` against Docker PostgreSQL and Mailpit | `.github/workflows/ci.yml:274`–`341`, step at `:317` |
| Their build path | Each app is its own Go module requiring `gorbital.dev@v0.2.1` and then replacing every module with a relative path into this checkout | `examples/apps/README.md` ("Layout"), `examples/apps/plateful/go.mod:111`–`131` |
| How the docs use them | 351 `<!-- include -->` markers under `docs/`; 345 name a path under `examples/apps` (171 in `docs/build`, all Plateful; 167 in `docs/examples`) | `grep -rn '<!-- include ' docs/` |
| How a marker resolves | `docscheck` reads the path relative to the checkout root — found by walking up to `docs/docs.json` — and fails when the file or its `docs:start` region is missing | `internal/tools/docscheck/main.go`, `repoRoot`, `checkInclude` |
| Other references | 39 further lines under `docs/` name `examples/apps` in prose or links, across 48 files in total | `grep -rn "examples/apps" docs/` |

Two things follow. The apps prove only that the library *in this checkout* works, because the `replace` directives point at it. And the tutorial chapters cannot be separated from `examples/apps/plateful` by hand-waving: 171 markers in `docs/build` resolve into that one directory, and a move that does not carry the include mechanism with it breaks the documentation build on the first commit.

## Options

### What moves

| Option | Verdict |
|---|---|
| Everything under `examples/` | Rejected: the golden apps generate `cli/internal/recipes/*` and are byte-compared in the same CI step. Moving them breaks template generation, which is the point of their existing |
| Nothing | Rejected: 1 637 files of documentation stay in the library repository, and its CI builds all six applications on every pull request |
| **Only `examples/apps`** | **Chosen**: it is exactly the set that nothing in the library generates from, compares against or requires to build |

### Repository layout

| Option | Verdict |
|---|---|
| `apps/<name>` | Rejected: an extra path segment in every documentation link and every README for no gain |
| **Top-level `<name>`** | **Chosen**: every path is one segment shorter, and future non-application content gets its own named directory — `clients/` for generated SDK samples, `deploy/` for deployment recipes |
| One repository per app | Rejected: six repositories to create, tag, release and keep in step with the library |

### How the documentation gets the code

| Option | Verdict |
|---|---|
| Vendor the snippets into the pages | Rejected: that is the drift the include mechanism exists to prevent (ADR-0084) |
| A git submodule | Rejected: submodules make every contributor's checkout harder, and the docs site's build already reads from git refs |
| **Fetch a pinned tag, in `docscheck` and in the docs build** | **Chosen**: one version to bump, and the same resolution locally and in CI |

### How history is handled

| Option | Verdict |
|---|---|
| A fresh repository with the files copied in | Rejected: loses the history of 1 637 files, including every review that shaped them |
| **`git subtree split`** | **Chosen** |

## Decision

### 1. What moves, and what stays

`examples/apps/plateful`, `invoicing`, `admin-tool`, `mobile-backend` and `payments` move to `github.com/gorbital/examples`, public, under the same licence and code of conduct.

`examples/apps/shelfie` **stays**, at `examples/shelfie`. It is the golden output `orb gen module` is compared against and the application the CLI's tests copy, so it is input to the build in the same way the golden apps are, and moving it breaks `go test -C cli ./...`. Its markers — 99 of them — resolve locally, like the five that name `examples/full-single`.

The golden apps stay too, unchanged and still byte-compared. `examples/README.md` is rewritten to say that what remains is template source and test fixtures, not documentation, and names shelfie and what it verifies. The `example apps` CI job is deleted, and `examples/shelfie` joins the `test`, `lint` and `govulncheck` matrices with the golden apps.

### 2. Layout

Top-level application directories, not an `apps/` subdirectory:

```text
github.com/gorbital/examples
├── README.md              what each app shows, and which library version it builds against
├── go.work                every app; point it at a local gorbital checkout to develop
├── .github/workflows/
│   └── ci.yml             matrix over */go.mod, nightly against gorbital main
├── plateful/              the restaurant platform — the flagship, built all the way through
├── byo-identity/          no gorbital sign-in, no gorbital organisations; the proof
├── mobile-backend/        modules/jwt wired by hand (unchanged)
├── payments/              signed webhooks, idempotency, InsertTx
├── invoicing/             (candidate for retirement, §9)
└── admin-tool/            (candidate for retirement, §9)
```

`shelfie` is not in that list: it stays in the library repository, at `examples/shelfie` (§1).

### 3. The move preserves history

In the library repository:

```bash
git subtree split --prefix=examples/apps -b examples-split
```

That branch becomes the new repository's root, so each app keeps its own commits at its new path. The files are then removed from the library repository in the same release.

### 4. Each app builds against the published library

Every app's `go.mod` drops its relative `replace` block and requires the published `gorbital.dev@v0.2.2` modules. A root `go.work` lets a contributor point all of them at a local checkout with one line.

This is strictly better than what exists today. The apps then prove that the **published** library works, not only the contents of one checkout.

### 5. CI in the new repository

A matrix over `*/go.mod` running `gofmt`, `go vet`, `go build` and `go test -race`, plus the Docker PostgreSQL and Mailpit suites the library uses. On pull requests it builds against the published library. Nightly it builds against `gorbital` `main`, so a library change that breaks an example is found within a day rather than at the next release.

### 6. The repositories are tagged in lockstep

`examples/v0.2.2` is tagged at the same time as the library's `v0.2.2`. Documentation can then pin a pair of versions known to build together, and a reader who checks out either tag gets the other's counterpart.

### 7. Includes resolve across repositories

`internal/tools/docscheck` keeps resolving `<!-- include <path>#<marker> -->` exactly as it does now for paths inside the checkout. For a path under `examples/`, it resolves against a checkout of the examples repository at the tag pinned in a new `docs/examples.json`, and fails when the tag is missing or when a marker has gone — the same failure a missing region gives today. A `make examples` target fetches the pinned tag, so `docscheck` runs the same way locally and in CI. The repository has no `Makefile` today; this adds one, alongside the existing `scripts/`.

The move and the include resolution land in the **same pull request**. Either alone breaks the documentation build: removing the files first leaves 345 markers pointing at nothing, and teaching `docscheck` to fetch a tag that does not exist yet fails just as hard.

### 8. Plateful is the flagship

Plateful, the restaurant delivery platform, is built all the way through and is the app the tutorial chapters teach. The 171 markers in `docs/build` already point at it; this makes that the stated shape rather than an accident.

### 9. Deferred, written down, not done

`invoicing` and `admin-tool` overlap Plateful and should be folded into it or retired. (`shelfie` overlaps it too, but it is a fixture the CLI's tests read, so retiring it is a different question and a harder one.) That is an editorial change to a dozen documentation pages and a judgement about what each chapter teaches. It must not ride along with a mechanical move, so it is not in this record's scope and gets its own decision. `mobile-backend` (41 files, `modules/jwt` wired by hand) and `payments` (36 files) are small and stay untouched.

## Consequences

- The library repository's CI gets faster: it stops building, vetting and testing six applications, with their PostgreSQL and Mailpit services, on every pull request.
- An example can now rot silently against an unreleased library, because a pull request to the library no longer builds them. The nightly job against `gorbital` `main` is the mitigation, and it moves the detection window from "immediately" to "within a day".
- The documentation build gains a cross-repository dependency and a pinned version in `docs/examples.json` to bump every release. A release that forgets it pins the previous examples tag, and `docscheck` fails when a marker has since moved.
- A contributor changing an application and the library together needs `go.work`. Before, the relative `replace` directives did it silently.
- 245 include markers move to the other repository and 99 (Shelfie's) are rewritten from `examples/apps/shelfie/…` to `examples/shelfie/…`; the five that name `examples/full-single` are untouched. Measured, not estimated: 344 markers named `examples/apps` before the move, across 39 files, with 49 further prose lines naming it.
- `examples/` in the library repository comes to mean two things, neither of them documentation: template source and test fixtures. The name is now narrower than it reads, which `examples/README.md` has to say plainly.
- **The `cli` module's tests are not run by `go test ./...` from the repository root.** `cli` is a separate Go module, so a change that breaks four of its test files passes every check a contributor is likely to run by hand. That is how this record came to classify `shelfie` as documentation, and how the reclassification survived a full verification pass. Anything that touches `examples/` has to run `go test -C cli ./...`, which takes minutes, not seconds.
