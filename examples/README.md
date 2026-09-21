# examples/ is template source, not documentation

Everything in this directory is **input to the build**. It is not a set of
samples to read, copy or tidy, and the name is narrower than it reads.

`go generate ./internal/recipes/` in `cli/` renders the `orb` CLI's templates
out of these applications, and CI byte-compares both the rendered templates
and the applications themselves. Move a file, rename a directory or hand-edit
a line here and the template generation produces a different tree, the
comparison fails, and `orb new` writes something nobody wrote on purpose.

| Directory | What generates from it |
|---|---|
| `minimal/` | `cli/internal/recipes/minimal` — the Minimal preset |
| `full-single/` | `cli/internal/recipes/full` — the Full preset, one tenancy |
| `full-multi/` | `cli/internal/recipes/full-multi` — the Full preset with organisations |
| `v0.1/full-single/`, `v0.1/full-multi/` | the v0.1 layout, frozen: `orb upgrade --layout v0.2` refuses an app that doesn't match them byte for byte (ADR-0083) |

`go generate` also copies the library's sign-in and organisations into the
golden Full apps, exactly as `orb new` does, so a change to `authhttp` or
`orgshttp` that these applications don't show fails in CI.

## Rules

- **Don't move or rename anything.** The generator and the comparison both
  address these paths directly, from `.github/workflows/ci.yml` and from the
  package doc of `cli/internal/recipes/recipes.go`.
- **Don't hand-edit the generated side.** Change the application here, run
  `go generate ./internal/recipes/` in `cli/`, and commit both.
- **Don't treat `v0.1/` as maintainable.** It is frozen. An upgrade path
  compares against it.
- `api/openapi.json` in each application is regenerated and compared in CI;
  after changing a route, run `go run ./cmd/api openapi > api/openapi.json`
  in that application.

## Where the example applications went

The applications the documentation includes code from — Plateful, Shelfie,
the recipes — are **not here any more**. They moved to their own repository
in v0.2.2 ([ADR-0093](../docs/adr/0093-the-examples-repository.md)):

<https://github.com/gorbital/examples>

Nothing in this library generates from them, compares against them or needs
them to build, and this repository's CI no longer builds all six applications
on every pull request. Their history moved with them.

A page includes their code by marker as before. `internal/tools/docscheck`
resolves a path under `examples/apps/` against a checkout of that repository
at the ref [`docs/examples.json`](../docs/examples.json) pins, which
`scripts/examples.sh` fetches into the gitignored `.examples/`:

```bash
scripts/examples.sh
go run -C internal/tools/docscheck .
```

A marker that names a golden app here — there are five, all
`examples/full-single` — still resolves in this checkout.
