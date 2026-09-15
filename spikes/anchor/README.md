# Spike: inserting code at `//orb:anchor` (text vs AST)

**Status:** done · **Date:** 2026-09-14 · **Decides:** the `insertLine@anchor` note in [ADR-0021](../../docs/adr/0021-generator-operation-model.md)

Throwaway code. Nothing outside `spikes/` may import it.

## Question

When `orb add` wires a feature into an owned file (for example `internal/app/modules.go`), should it insert the call line with **parser-located text insertion** or by **editing the syntax tree** with `dave/dst`?

## Method

Two implementations with the same contract, `Insert(src, anchor, stmt) → (out, outcome, err)`:

| Implementation | How |
|---|---|
| **InsertText** ([text.go](text.go)) | `go/parser` finds the real anchor comment (not text in strings) and the enclosing block; the statement is inserted as a line after the anchor block; `go/format` formats and validates |
| **InsertDST** ([dst.go](dst.go)) | `dave/dst` decorated tree: find the statement the anchor comment is attached to, insert a parsed statement, move comment and blank-line decorations |

Shared rules ([anchor.go](anchor.go)): exactly one statement allowed; idempotency by canonical statement comparison; missing or duplicate anchors are errors (never guess).

10 scenarios run against both ([anchor_test.go](anchor_test.go)). Each checks the expected text, that exactly one line was added, that no existing line changed, that the result parses, and that re-applying changes nothing.

```bash
GOTOOLCHAIN=local go test -v ./...
```

Environment: Go 1.25.3, dave/dst v0.27.3, macOS arm64.

## Results

| Scenario | Text | DST |
|---|---|---|
| Empty anchor block | ✅ | ✅ |
| Second module keeps order | ✅ | ❌ inserts extra blank lines |
| Anchor at end of block | ✅ | ❌ adds a blank line above the anchor |
| Anchor alone in a nested empty block | ✅ | ❌ unsupported: the comment is attached to the block, not a statement |
| User edits and comments preserved | ✅ | ❌ extra blank line |
| Missing anchor → error | ✅ | ✅ |
| Duplicate anchor → error | ✅ | ✅ |
| Anchor text only inside a string literal → not matched | ✅ | ✅ |
| Invalid statement → error, file unchanged | ✅ | ✅ |
| CRLF line endings | ✅ | ✅ |
| **Total** | **10 / 10** | **6 / 10** |

| Measure | Text | DST |
|---|---|---|
| Code (non-blank, non-comment lines) | 63 (+72 shared) | 157 (+72 shared) |
| Dependencies | Standard library only | `dave/dst` + 11 transitive modules (several `golang.org/x` versions from 2021–2022) |
| Go version | Any supported | `dst` v0.28.0 requires Go ≥ 1.26; v0.27.3 needed for Go 1.25 |

## Findings

1. **Text insertion located by the parser is enough.** The parser gives exact comment positions and block boundaries, so string literals can't cause false matches, and `go/format` guarantees valid, formatted output. The insertion is a one-line diff, which suits the 3-way merge upgrade model (merge spike).
2. **AST editing moves the difficulty into comment decorations.** Where a comment "belongs" (before a statement, after one, or on the block itself) depends on surrounding blank lines. Each failed scenario needs its own decoration handling. The failures are fixable, but each fix is another edge case to maintain.
3. **`dst` adds dependency and toolchain risk:** its latest release forces Go 1.26, and its module graph pins old `golang.org/x` versions.
4. **Diff minimality matters more than tree purity.** Upgrades and code review depend on one-line changes; text insertion delivers exactly that.

## Decision input for ADR-0021

`insertLine@anchor` uses **parser-located text insertion**, standard library only:

- locate the anchor with `go/parser` (comments only), require exactly one;
- insert after the anchor block, matching the anchor's indentation and line endings;
- skip if an equivalent statement already exists in the block (canonical `go/printer` comparison);
- validate with `go/format`; on failure, write nothing and report the error;
- no AST-rewriting dependency.

## Not covered

- Multi-statement insertions (not needed: recipes insert one call line; everything else is a new file)
- Files that don't parse before insertion (the operation refuses them; the error tells the user to fix the file first)
