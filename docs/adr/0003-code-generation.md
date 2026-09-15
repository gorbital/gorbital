# ADR-003: Code generation

**Status:** Accepted (2026-09-14), amended by ADR-0021 and the merge spike (spikes/merge/README.md)

**Context:** Generated code must stay customisable and upgradeable without overwrites.

**Options:** (a) One-shot templates; (b) always-regenerated code with "protected regions"; (c) three code classes (library / derived / scaffold) + typed operations + lock-file baseline + 3-way merge.

**Decision:** (c). Templates create files, AST edits at visible anchors modify owned files, `gorbital.lock` records baselines.

**Why:** (a) has no upgrades. (b) protected regions break under refactoring and feel like fighting the tool. (c) matches git mental models and is proven by Copier/cruft.

**Tradeoffs:** Merge conflicts are possible; lock file must be committed; generator is more complex than a template copier.

**Consequences:** Every recipe needs idempotency and upgrade tests; the reference app golden test is mandatory in CI.
