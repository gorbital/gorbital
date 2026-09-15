# Spike: 3-way merge for `orb upgrade`

**Status:** done · **Date:** 2026-09-14 · **Decides:** [ADR-003 Code generation](../../docs/adr/0003-code-generation.md)

Throwaway code. Nothing outside `spikes/` may import it.

## Question

Can `orb upgrade` carry a template change into a generated file the developer has edited, without losing their edits?

## Method

- `Upgrade(base, ours, theirs)` in [merge.go](merge.go):
  - `base`: what the generator wrote at the old version
  - `ours`: the file on disk, edited by the developer
  - `theirs`: what the new template version writes
- Shortcuts: if `ours == base`, take `theirs`. If `base == theirs`, keep `ours`.
- Otherwise run `git merge-file -p` with labelled conflict markers (`yours` / `gorbital upgrade`).
- `InsertAfterAnchor` mimics `orb add` inserting a line at `//orb:anchor <name>` (text level only; the AST version is a separate spike).
- Test file: an `app.go` template in "release 1" and "release 2" (release 2 adds a security-headers handler).

Run it:

```bash
go test -v ./...
```

Environment: Go 1.25.3, git 2.50.1, macOS arm64.

## Results

| Scenario | Outcome | Edits lost? | Output parses? |
|---|---|---|---|
| Developer never edited the file | take-theirs | n/a | yes |
| Template unchanged, developer edited | keep-ours | no | yes |
| Developer edits in a different region from the template change | **clean merge** | no | yes |
| Developer and template change the same line | conflict (1), clear labelled markers | no | n/a |
| `orb add` inserted a module line; base and theirs rebuilt by replaying the insert | **clean merge** | no | yes |
| Same, but base is the bare template (no replay) | clean merge in this case | no | yes |
| Developer adds a line directly next to the template's changed line | **conflict (1)** | no | n/a |

In every scenario each developer line and each template line appears **exactly once** in the result. No edit was lost.

## Findings

1. **The approach works.** ADR-003's 3-way merge holds up: untouched files update automatically, separate edits merge cleanly, and overlapping edits become normal git conflicts with the developer's code preserved.
2. **Line-based merges treat neighbouring lines as overlapping.** A developer line directly above a changed template line conflicts, even though the changes are logically independent. This is how git works and can't be fixed in the merge itself.
3. **So scaffold churn must be kept small.** The release-2 change here (wrapping `mux` in `SecureHeaders`) is exactly the kind of change that belongs in the library instead: `httpx.NewServer` could apply secure headers by default, so the template never changes. This confirms "thin glue, thick library" as a hard rule.
4. **Replay the recorded operations to rebuild `base`.** The naive base merged cleanly here only because the inserted module line was far from the template change. Without replay, every line `orb add` inserted counts as a developer edit, so conflicts get more likely and `orb remove` can't tell generated lines from hand-written ones. `base` and `theirs` must both be rendered by replaying the operations recorded in `gorbital.lock`.
5. **A conflicted file doesn't compile.** `orb upgrade` must work on a branch, stop before running `go build`, and list the conflicted files clearly.

## Rules this adds to ADR-003

- Scaffold templates must render gofmt-formatted output, so formatting alone never causes a diff.
- Before changing a scaffold template, ask whether the change can ship in a library instead. Scaffold template changes need a changelog entry and an upgrade test.
- `gorbital.lock` must record every applied operation (recipe, version, anchor, inserted code), not just file hashes, so `base` and `theirs` can be replayed.
- `orb upgrade` runs on a branch, reports conflicts per file, and doesn't run the build step until conflicts are resolved.

## Not covered (follow-up spikes)

- Anchor insertion through the AST (`dave/dst`) with comments preserved
- Many files and full-project replay
- Real gofmt'd tab-indented output
- Renamed or deleted scaffold files
- Upgrade UX on a real git branch
