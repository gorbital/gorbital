# ADR-008: Configuration

**Status:** Proposed

**Context:** Config systems tend to accumulate precedence rules and formats.

**Options:** Viper-style layered files; env-only; struct + env + optional `.env` in dev.

**Decision:** Typed structs per module, env vars as source of truth, `*_FILE` for secrets, `.env` in dev only, fail-fast validation, no runtime-mutable config.

**Why:** One precedence rule anyone can remember; works identically on every host.

**Tradeoffs:** Long env var lists for big apps; no structured config files.

**Consequences:** Dashboard-managed configuration is out of scope; runtime settings are modelled as data.
