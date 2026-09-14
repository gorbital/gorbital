# ADR-006: Authentication

**Status:** Proposed

**Context:** Auth is high value and high risk; fixes must reach existing apps.

**Options:** Generate all auth code into apps; external IdP only; embedded library with scaffolded handlers.

**Decision:** Embedded library (hashing, tokens, sessions, stores) + owned handlers/templates. v1: password, server-side sessions, verification, reset, throttling, RBAC. No JWT sessions. OIDC login v1.1, TOTP v1.2, passkeys v2. Enterprise SSO delegated.

**Why:** Security patches ship via `go get`; UI and flows stay customisable.

**Tradeoffs:** Customising deep internals means forking the module or implementing interfaces.

**Consequences:** The auth module needs security review policy, advisories and long-term maintainers before 1.0.
