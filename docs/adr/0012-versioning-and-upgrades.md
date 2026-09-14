# ADR-012: Versioning and upgrades

**Status:** Superseded by ADR-0015 and ADR-0016 (2026-09-14)

**Context:** Apps generated on old versions must reach new ones.

**Options:** Regenerate and diff manually; upgrade guides only; layered mechanism by change type.

**Decision:** Semver everywhere; behaviour in libraries (`go get`); `//go:fix inline` deprecations; shipped analyzers for majors; 3-way merge for scaffolds on a branch; append-only migrations; compatibility matrix; upgrade of previous reference app gated in CI.

**Why:** Each change type has the cheapest safe path; never destroys developer changes.

**Tradeoffs:** Significant investment in upgrade tooling before features.

**Consequences:** "How do existing apps get this?" is a required section in every feature PR.
