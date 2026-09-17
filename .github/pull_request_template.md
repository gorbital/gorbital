## What and why

<!-- What this changes and the reason. Link the issue or decision record. -->

## How existing apps receive it

<!-- ADR-0016: `go get`, `orb upgrade`, a manual step, or nothing. Add a row to docs/guides/upgrade-notes.md when apps are affected. -->

## Checklist

- [ ] Tests added or updated, and run in every module touched
- [ ] Public surface: none changed, or an accepted ADR is linked and the API listing and surface inventory are updated (additions only)
- [ ] Golden apps changed in both Full apps where relevant; `go generate ./internal/recipes/` and `go run ./cmd/api openapi --dir api` re-run
- [ ] Docs: the guide that describes the behaviour, upgrade notes and changelog updated
- [ ] New exported identifiers have doc comments and `Example` functions, and the Methods pages are regenerated (ADR-0084)
- [ ] An example app and its chapter show the feature, when it is something developers use directly
- [ ] Target branch is right: `main` (latest release), `release/v0.1` (v0.1 fixes only) or the v0.2 line
- [ ] No secrets, emails or tokens in logs, errors or fixtures
