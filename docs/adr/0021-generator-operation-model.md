# ADR-0021: Generator operation model

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0003 · **Supersedes (with ADR-0019):** ADR-0002

## Context

The CLI creates apps (`aps new`) and adds features (`aps add`) into code the developer owns, and must later upgrade that code without losing edits. The merge spike showed that 3-way merges work, that neighbouring-line edits conflict, and that the merge base must be rebuilt by replaying operations. Editing shared owned files at many anchors multiplies conflicts.

## Options

1. Regenerate owned files wholesale.
2. Templates plus AST edits at many anchors in shared files.
3. A small fixed set of declarative operations, one owned wiring file per feature, a single anchor line per feature, and an operation log in `apistock.lock`.

## Decision

Option 3.

### Engine

```text
recipes + project state
  1 RESOLVE   presets, dependencies, tenancy, version compatibility
  2 PLAN      typed operations, no file I/O
  3 RENDER    text/template → gofmt → go/parser validation
  4 CHECK     per operation: applied / already done (idempotent) / conflict
  5 PREVIEW   diff; risky new imports highlighted (os/exec, net, unsafe, plugin, syscall)
  6 APPLY     writes confined with os.Root; temp file + rename
  7 RECORD    apistock.lock: recipe, version, each operation and its inputs, base hashes
```

`aps new` and `aps add` use the same engine. `aps new` = `base-minimal` + selected feature recipes.

### Operations (fixed vocabulary, versioned)

| Operation | Purpose |
|---|---|
| `createFile` | New file from a template |
| `insertLine@anchor` | One call line after `//aps:anchor <name>` in an owned file |
| `addRequire` | Go module requirement or `tool` directive |
| `copyMigration` | Module migration into `db/migrations` with a timestamp |
| `appendEnv` | Documented entry in `.env.example` |

No operation executes code, runs shell commands, downloads files or writes outside the project.

### Ownership model

| Code | Tracked for upgrades? |
|---|---|
| Recipe output (base app, `infra_*.go`, module wiring, auth/orgs/ops modules, emails) | **Yes**: operations recorded and replayed for 3-way merges (ADR-0016) |
| `aps gen resource` output | **No**: one-shot starting point |
| Derived code (`internal/db`, `api/openapi.json`) | Regenerated from sources, never merged |

### Rules

- **One wiring file per feature** and **one line at one anchor** in `app.go`/`modules.go`; everything else is new files.
- **Idempotent:** running any command twice produces no second change (CI test per recipe).
- **Missing anchor:** stop, print the exact line to add; never guess.
- **Input safety:** names must satisfy `go/token.IsIdentifier`; field types from an allowlist; no raw user strings in generated code.
- **Clean git tree required** (override with `--allow-dirty`); `--dry-run` and `--json` on every mutating command.
- **Resource templates** come in variants selected from `apistock.yaml` (`resource/single`, `resource/org`) with `--scope=org|user|global`.

## Why

A minimal edit surface means fewer conflicts and simpler removal; replaying operations makes upgrades reliable; declarative operations make recipes safe to run from third parties.

## Trade-offs

- More files in `internal/app`.
- The operation vocabulary will need careful, versioned extension.

## Consequences

- Decided by the [anchor-edit spike](../../spikes/anchor/README.md): `insertLine` uses parser-located text insertion (standard library only), validated with `go/format`; no AST-rewriting dependency.
- Golden tests: the generator must reproduce `examples/minimal`, `examples/full-single` and `examples/full-multi` exactly.
