# ADR-011: GitHub integration

**Status:** Proposed

**Context:** Creating repos and CI is convenient but token handling is a liability.

**Options:** apistock GitHub App / OAuth; CLI device flow storing tokens; delegate to `git` and `gh`.

**Decision:** Delegate to `git` and `gh`; generate plain CI files; private by default; no apistock-held tokens in v1.

**Why:** No credentials to protect, no platform dependency, forge-portable.

**Tradeoffs:** Requires `gh` for one-command repo creation; fallback prints commands.

**Consequences:** Any future hosted GitHub App belongs to the control plane, not the CLI.
