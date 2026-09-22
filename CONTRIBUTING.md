# Contributing to gorbital

Thank you for helping. This guide covers how to propose a change, set up the repository, and get a pull request merged. How decisions are made: [GOVERNANCE.md](GOVERNANCE.md).

## Before you write code

- **Bugs:** open an issue with the version, what you ran and what happened. Security issues never go in public issues: see [SECURITY.md](SECURITY.md).
- **Small fixes** (typos, docs, tests, clear bugs): send a pull request directly.
- **Features and public surfaces:** open an issue first. Anything that adds or changes a public surface (exported Go API, error codes, audit actions, permissions, setting keys, job names, `/ops` responses, CLI flags or `--json` output) needs a decision record in `docs/adr/`, accepted before the code merges. Copy the shape of a recent one, such as [ADR-0052](docs/adr/0052-shared-rate-limits.md).
- Read [the architecture](docs/architecture.md) and its rules first: thin glue, thick library; modules never import other modules; only PostgreSQL is required.

## Set up

You need Go 1.26 or later and Docker.

```bash
git clone https://github.com/gorbital/gorbital.git
cd gorbital
docker compose up -d --wait        # PostgreSQL on 55432 and Mailpit on 51025/58025, for tests
export GORBITAL_TEST_DATABASE_URL='postgres://gorbital:gorbital@127.0.0.1:55432/gorbital?sslmode=disable'
export GORBITAL_TEST_MAILPIT_SMTP=127.0.0.1:51025 GORBITAL_TEST_MAILPIT_URL=http://127.0.0.1:58025
```

The repository holds several Go modules: the core at the root, one per directory under `modules/`, the CLI in `cli/`, and the golden apps in `examples/`. Test each one from its own directory:

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

More in [Testing](docs/guides/testing.md).

## How the pieces fit

| You change | Also do |
|---|---|
| A library package or module | Doc comments on every exported identifier; an `Example` function for new API; tests; update the API listing if you added API (see [Stability](docs/guides/stability.md)); regenerate the Methods pages (see [Documenting methods](#documenting-methods)) |
| A golden app (`examples/full-single`, `examples/full-multi` on `gorbital.Main`, `examples/minimal`; the v0.1-layout `examples/v0.1/*` take fixes only) | Make non-organisation changes in both Full apps; after endpoint changes run `go run ./cmd/api openapi --dir api` in the app; then `cd cli && go generate ./internal/recipes/`. Never edit `cli/internal/recipes/{minimal,full,full-multi,v0.2}` by hand |
| Error codes, audit actions, permissions, settings or jobs in a Full app | Record the app's own names in `api/surface.json` (`go test ./internal/modules -run TestPublicSurface -update` in both Full apps; `./internal/app` in `examples/v0.1/*`) and regenerate the reference pages with `go run -C internal/tools/refdocs . -write` (it needs the test database; describe a new audit action in `internal/tools/refdocs/descriptions.json`). See [Stability](docs/guides/stability.md) |
| A migration | Add a new file; released migrations never change |
| Anything a generated app receives | A row in [upgrade notes](docs/guides/upgrade-notes.md) saying what existing apps must do, and a [changelog](CHANGELOG.md) entry |
| Behaviour users see | The guide that describes it, under `docs/` |

## Branches and release lines

| Branch | For |
|---|---|
| `main` | The latest release, and fixes to it |
| `release/v0.1` | Security fixes and factual corrections to v0.1 only, cherry-picked and listed in the changelog |
| `v0.2` | The v0.2 line ([roadmap](docs/v0.2-roadmap.md)); work lands through `framework/phase-N` branches |

Documentation follows the same branches: the docs site serves each line from its branch ([ADR-0084](docs/adr/0084-versioned-documentation.md)). A change to how developers use gorbital documents itself in the same pull request: the guide, the Methods page (doc comments and `Example` functions), the example app and its chapter when it shows the feature, upgrade notes and changelog.

## Releasing

A release tags the root module, every module under `modules/`, `gorbital/` and the CLI at one version on one commit of `main`. **A tag that reaches the module proxy cannot be moved or deleted**, so every check runs before the push.

The mistake to avoid is a module that requires a sibling at an older version. The `replace` directives in this repository point at the checkout, so everything builds here whatever the requirements say; a consumer has no `replace` directives and gets the version the requirement names. `v0.3.0` shipped 18 modules requiring `v0.2.1`, and `gorbital.dev/gorbital@v0.3.0` did not build for anyone who downloaded it.

**The bump belongs in the release commit, not in an ordinary pull request.** Between releases every `gorbital.dev` requirement, and `recipes.LibraryVersion`, name the last *published* version, because `orb new` generates an app outside this repository: it has no `replace` directives and resolves from the module proxy, so a version the proxy does not have yet fails `go mod tidy` in every generated app. Naming the new version is therefore the last thing that happens before the tags are pushed, and it is why a release is its own commit.

1. **Point every requirement at the new version** and commit the result as the release commit, on a branch that is **not** under `release/`: the `release lines` ruleset reserves those for the maintenance lines in the table above and only lets a merged pull request update them, so a release-prep branch there can be created and then never pushed to again. `framework/release-vX.Y.Z` works. Nothing in the release is edited by hand.

   ```bash
   scripts/set-requirements.sh v0.3.2
   git diff            # 22 go.mod files and recipes.LibraryVersion
   ```

   CI passes on that commit even though the version it names is not published: `orb new --local` and the golden apps replace **every** gorbital module with the checkout, so nothing resolves from the proxy. If a job does fail on `unknown revision`, a `replace` is missing rather than the release being early — add it instead of pushing the tag to make the error go away.

2. **Run the checks in CI, before any tag exists.** In Actions, run *Release library* with "Run workflow" and the version. Its `requirements` job must be green. Running `scripts/set-requirements.sh v0.3.2 --check` locally proves the same thing, but the workflow leaves a record that the release was checked.

3. **Create the tags**, which repeats the requirements check and refuses a dirty tree or a commit that isn't on `origin/main`.

   ```bash
   scripts/release.sh v0.3.2 --check   # nothing is created
   scripts/release.sh v0.3.2 --push
   ```

4. **Watch the workflows.** Each tag push runs `requirements` again and then `consumer`, which does `go get` and `go build` in an empty module with no `replace` directives and no workspace: the only check that sees what a consumer sees. A red `consumer` job means the version is broken for everybody, and the fix is a new version, not a moved tag.

5. **Publish the GitHub release.** *Release orb* builds and signs the binaries and leaves them on a **draft** release, so the tag is on the proxy while GitHub still shows the previous version as Latest. Nothing publishes it for you.

   ```bash
   gh release view v0.3.2 --json isDraft,assets   # 8 assets, draft=true
   gh release edit v0.3.2 --draft=false --latest --notes-file notes.md
   ```

   Write the notes from the version's changelog section; the draft arrives with an empty body. `v0.3.1` and `v0.3.2` were each published hours late because this step is easy to miss after the tags succeed.

6. **Retract a broken version** in the next release rather than leaving it resolvable. Add it to every affected module's `go.mod` with a rationale `go get` can show, and say in the changelog which version replaces it.

   ```
   // v0.3.0 requires gorbital.dev modules at v0.2.1, so gorbital.dev/gorbital
   // does not build and the rest resolve a combination that was never tested.
   // v0.3.1 is the same code with the requirements corrected.
   retract v0.3.0
   ```

   A `retract` only reaches anyone through a **later** version, so the release that adds it must itself be tagged and pushed.

## Documenting methods

The [Methods](docs/methods/index.md) tab of the docs has a page for every package of the library, generated from its source: signatures, doc comments, `Example` functions and the release each identifier arrived in. What you write in Go is what readers see.

- **Every exported identifier has a doc comment:** constants and variables (a comment on the group counts), functions, types and methods. Start with the name, say what it does, and for functions that fail say which errors they return and when. Use [doc links](https://go.dev/doc/comment#doclinks) (`[Server.Run]`, `[httpx.Mapper]`); they become links between Methods pages.
- **New functions, types and methods have an `Example` function** in the package's `example_test.go` (`ExampleNew`, `ExampleServer_Run`, `ExampleServer_Run_shutdown` for a second one), with an `// Output:` comment when the output is deterministic so `go test` runs it. API released in `v0.1.0` is exempt for now (adding examples to it is welcome), and so are methods a standard interface defines: `Error`, `String`, `Write`, `WriteHeader`, `ServeHTTP` and `Unwrap`.
- **Curated text** that isn't a doc comment, such as when to use a package and links to guides, goes in `internal/tools/refdocs/overlay/methods/<slug>.md` (`httpx.md`, `modules-auth.md`). It appears after the package doc; links are relative to `docs/methods/`.
- **Regenerate** the pages and commit them with the change. A new package also needs a page in the Methods tab of `docs/docs.json`.

```bash
go run -C internal/tools/refdocs . -methods -write   # regenerate docs/methods
go run -C internal/tools/refdocs . -methods          # check, as CI does
```

The check needs no database. It fails with `file:line identifier` for an exported identifier without a doc comment or new API without an `Example`, with `docs/methods/<page>.md: stale` when the pages don't match the source, and when `docs/docs.json` doesn't list a page.

Examples of whole apps, as opposed to single functions, are pages in the Examples tab: see [Writing an example](docs/examples/README.md).

## New error codes and audit actions

Apps generated by `orb` v0.1.0 record the problem codes and audit actions of every `gorbital.dev` package they link in `api/surface.json`, and their `TestPublicSurface` fails on a name it hasn't recorded. A new code or action in a package they already link breaks them on `go get`. **Put new codes and actions in a new package**, such as `gorbital.dev/httpx/timeout` for `request_timeout`, or in `gorbital.dev/gorbital`, never in the packages v0.1 apps link: `actor`, `app`, `audit`, `buildinfo`, `config`, `health`, `httpx`, `mail`, `page`, `ratelimit`, `requestid` and `webhook` in the root module, and `modules/auditpg`, `auth` (with `auth/passkey` and `auth/social`), `devconsole`, `flags`, `idempotency`, `jobs`, `mail/resend`, `mail/smtp`, `mail/suppressionpg`, `observability`, `openapi` (with `openapi/reference`), `orgs`, `postgres`, `ratelimitpg`, `releases`, `settings`, `storage` (with `storage/local`, `storage/logarchive` and `storage/s3`) and `telemetry`. The list and the reasons are in [stability](docs/guides/stability.md#adding-error-codes); the v0.1.0 scaffold compatibility check (`ORB_COMPAT=1 ORB_COMPAT_FROM=v0.1.0 ORB_COMPAT_PUBLISHED=1`) proves it.

## Pull requests

- One change per pull request; explain the why in the description and fill in the template.
- Commit subjects are imperative and specific ("Share rate limits across instances"), with the reason in the body.
- All checks must pass: formatting, vet, tests with the race detector, lint, `govulncheck`, gitleaks, generated templates, API files and reference pages up to date, API and surface checks.
- Security-sensitive areas need two approvals ([GOVERNANCE.md](GOVERNANCE.md)).
- By contributing you agree your work is licensed under the [Apache License 2.0](LICENSE), the project's licence.

## Style

Match the code around you: readable over clever, standard library first, constructors with functional options, small consumer-owned interfaces, sentinel errors wrapped with context, hand-written SQL with one statement per repository file, logs that carry IDs and never emails, tokens or secrets. Documentation is plain and direct: say what something does and why, show the command, skip the marketing.
