# ADR-0084: Versioned documentation, Methods and Examples

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0015 (documentation of public API) · **Builds on:** ADR-0081

## Context

docs.gorbital.dev shows one set of documentation, synced from gorbital's `main` into gorbital-web by `pnpm sync-docs` and deployed from gorbital-web's `main`. `v0.1.0` is published; v0.2 changes how apps are written (ADR-0081). Readers on v0.1 must keep reading what applies to them, and v0.2 must be documented while it is built, not after.

Two gaps exist regardless of versions:

| Gap | Today |
|---|---|
| Public Go API | `api/*.txt` lists signatures for the compatibility check; readers use pkg.go.dev, which has no guides, no "since" and no curated context |
| Real applications | Guides explain features one at a time; the golden apps are complete but not explained step by step |

## Options

### Versions

| Option | Verdict |
|---|---|
| Edit one set of docs in place | Rejected: v0.1 readers lose their docs the day v0.2 docs land |
| Separate sites per version | Rejected: two deployments, two search indexes, links that break across them |
| **One site serving each version from its git ref: the latest release unprefixed, every version at `/<prefix>/`, the unreleased line at `/next/`** | **Chosen** |

### API reference

| Option | Verdict |
|---|---|
| Link to pkg.go.dev | Rejected as the only reference: no "since", no guides, no check that new API is documented |
| Hand-written pages | Rejected: drift |
| **Generated Methods pages from doc comments and `Example` functions, with curated overlay text, checked in CI** | **Chosen** |

### Examples

| Option | Verdict |
|---|---|
| Code blocks written in the Markdown | Rejected: they rot silently |
| **Runnable apps under `examples/apps/`, built and tested in CI, whose code the pages include by marker** | **Chosen** |

## Decision

### 1. Branches

| Branch (gorbital and gorbital-web) | Holds | Changes |
|---|---|---|
| `release/v0.1` | v0.1.0 code and docs as published (gorbital `41f7c6b`, gorbital-web `d10265d`) | Security fixes and factual corrections only, cherry-picked, listed in the v0.1 changelog |
| `v0.2` | The v0.2 line | Phases merge here |
| `framework/phase-N` | One phase | Its roadmap items |
| `main` | The latest release | Moves at each release |

Each future release line gets its own `release/vX.Y` when the next line starts.

### 2. The site

- `versions.json` in gorbital-web lists each version: id, label, git ref, URL prefix, status (`latest`, `older`, `preview`). Exactly one is latest.
- `sync-docs` reads each version's files from its git ref in the gorbital repository (never checking it out) into `apps/docs/content/<id>/`.
- The latest version is served at today's unprefixed URLs, so no v0.1 link changes, and also at `/<prefix>/` with a canonical link to the unprefixed page. Other versions exist only under their prefix. The sitemap lists the latest unprefixed and every other version.
- A version switcher on every page keeps the same page when it exists in the target version. An older version shows a banner linking to the latest; a preview shows "not released yet".
- Tabs come from each version's `docs/docs.json`, so a version without Methods or Examples doesn't show them.

### 3. Methods

- `internal/tools/refdocs` generates `docs/methods/<package>.md` for every public library package: package doc, identifiers grouped as `go doc` groups them, signatures, doc comments rendered as Markdown, `Example` functions with their output, the version the identifier arrived in (from frozen API listings per release), and optional curated text from `internal/tools/refdocs/overlay/methods/`.
- CI fails when a page is stale, when an exported identifier has no doc comment, or when an identifier new in the unreleased version has no `Example`. Identifiers released in v0.1.0 are exempt from the example rule until they are next changed.

### 4. Examples

- Each example is a complete app under `examples/apps/<name>/`, with its own `go.mod` replacing the library with the checkout, built, vetted and tested in CI.
- Pages under `docs/examples/` include code with `<!-- include examples/apps/<name>/<file>#<marker> -->`; the file marks regions with `// docs:start <marker>` and `// docs:end <marker>` (`--` or `#` comments for SQL, YAML and shell). `sync-docs` resolves includes from the same git ref and fails on a missing file or marker.
- The running example is **Shelfie**, a reading-tracker API; smaller **recipes** show one scenario each. Each roadmap phase adds the chapters its features make possible.

### 5. What a change documents

| Change | Documentation in the same pull request |
|---|---|
| Exported Go API | Doc comment, `Example` for new identifiers, regenerated Methods pages |
| Behaviour developers use | The guide that describes it |
| A feature an example can show | The example app and its chapter |
| A decision | An ADR |
| Anything an existing app receives | Upgrade notes and changelog |

The pull request template lists these.

## Why

- Git refs are already the source of truth for code versions; using them for docs keeps each version exactly what shipped.
- Generated reference and included example code can't drift from the code, and CI enforces both.

## Trade-offs

- The docs build grows with each version kept; old versions can be archived to a static export when that matters.
- The example rule applies only to new API, so v0.1 packages have gaps in examples until they are touched.
- Fixes to v0.1 docs need a cherry-pick to `release/v0.1`.

## Consequences

- gorbital-web's `main` keeps deploying the latest release; `/next/` needs a preview deployment of gorbital-web's `v0.2` branch.
- Contributors document methods and examples as part of review (CONTRIBUTING.md).

## Implementation

[v0.2 roadmap](../v0.2-roadmap.md), Phase 0 items 8–13 and the documentation rules in every phase.
