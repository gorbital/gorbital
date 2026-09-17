## When to use

These are building blocks, not a complete sign-in system: a Full app's `internal/modules/auth` owns the flows, tables and SQL and calls this package for the security-sensitive steps (hashing, tokens, codes, the session middleware, the permission catalog). Sign-in providers are in [modules/auth/passkey](modules-auth-passkey.md) and [modules/auth/social](modules-auth-social.md).

- [Authentication guide](../guides/authentication.md): the flows a Full app builds on these helpers.
- [API keys and service accounts](../guides/api-keys.md).
