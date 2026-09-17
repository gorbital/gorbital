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
| A golden app (`examples/full-single`, `examples/full-multi`, `examples/minimal`) | Make non-organisation changes in both Full apps; after endpoint changes run `go run ./cmd/api openapi --dir api` in the app; then `cd cli && go generate ./internal/recipes/`. Never edit `cli/internal/recipes/{minimal,full,full-multi}` by hand |
| Error codes, audit actions, permissions, settings or jobs in a Full app | Record them in `api/surface.json` (`go test ./internal/app -run TestPublicSurface -update` in both Full apps) and regenerate the reference pages with `go run -C internal/tools/refdocs . -write` (it needs the test database; describe a new audit action in `internal/tools/refdocs/descriptions.json`). See [Stability](docs/guides/stability.md) |
| A migration | Add a new file; released migrations never change |
| Anything a generated app receives | A row in [upgrade notes](docs/guides/upgrade-notes.md) saying what existing apps must do, and a [changelog](CHANGELOG.md) entry |
| Behaviour users see | The guide that describes it, under `docs/` |

## Documenting methods

The [Methods](docs/methods/index.md) tab of the docs has a page for every package of the library, generated from its source: signatures, doc comments, `Example` functions and the release each identifier arrived in. What you write in Go is what readers see.

- **Every exported identifier has a doc comment:** constants and variables (a comment on the group counts), functions, types and methods. Start with the name, say what it does, and for functions that fail say which errors they return and when. Use [doc links](https://go.dev/doc/comment#doclinks) (`[Server.Run]`, `[httpx.Mapper]`); they become links between Methods pages.
- **New functions, types and methods have an `Example` function** in the package's `example_test.go` (`ExampleNew`, `ExampleServer_Run`, `ExampleServer_Run_shutdown` for a second one), with an `// Output:` comment when the output is deterministic so `go test` runs it. API released in `v0.1.0` is exempt for now; adding examples to it is welcome.
- **Curated text** that isn't a doc comment, such as when to use a package and links to guides, goes in `internal/tools/refdocs/overlay/methods/<slug>.md` (`httpx.md`, `modules-auth.md`). It appears after the package doc; links are relative to `docs/methods/`.
- **Regenerate** the pages and commit them with the change. A new package also needs a page in the Methods tab of `docs/docs.json`.

```bash
go run -C internal/tools/refdocs . -methods -write   # regenerate docs/methods
go run -C internal/tools/refdocs . -methods          # check, as CI does
```

The check needs no database. It fails with `file:line identifier` for an exported identifier without a doc comment or new API without an `Example`, with `docs/methods/<page>.md: stale` when the pages don't match the source, and when `docs/docs.json` doesn't list a page.

Examples of whole apps, as opposed to single functions, are pages in the Examples tab: see [Writing an example](docs/examples/README.md).

## Pull requests

- One change per pull request; explain the why in the description and fill in the template.
- Commit subjects are imperative and specific ("Share rate limits across instances"), with the reason in the body.
- All checks must pass: formatting, vet, tests with the race detector, lint, `govulncheck`, gitleaks, generated templates, API files and reference pages up to date, API and surface checks.
- Security-sensitive areas need two approvals ([GOVERNANCE.md](GOVERNANCE.md)).
- By contributing you agree your work is licensed under the [Apache License 2.0](LICENSE), the project's licence.

## Style

Match the code around you: readable over clever, standard library first, constructors with functional options, small consumer-owned interfaces, sentinel errors wrapped with context, hand-written SQL with one statement per repository file, logs that carry IDs and never emails, tokens or secrets. Documentation is plain and direct: say what something does and why, show the command, skip the marketing.
