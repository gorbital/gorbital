# ADR-009: Repository strategy

**Status:** Accepted (2026-09-14), amended by ADR-0019 (core module at repository root)

**Context:** Libraries and templates drift when versioned apart; users shouldn't download every dependency.

**Options:** Single monorepo single module; multi-repo; hybrid monorepo with multiple Go modules + separate repos for index and control plane.

**Decision:** Hybrid. `orb` monorepo with core + each official module as its own Go module, recipes beside their module; `gorbital-index` and `gorbital-console` separate.

**Why:** Atomic cross-cutting changes and one CI, without dependency bloat for users.

**Tradeoffs:** Prefixed tags and release tooling are needed.

**Consequences:** Release automation is a Phase 1 deliverable, not an afterthought.
