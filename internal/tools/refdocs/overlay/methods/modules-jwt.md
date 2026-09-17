## When to use

When users sign in with an external identity provider (Auth0, Clerk, Supabase, Firebase, Cognito) and your API receives its access tokens. For gorbital's own sessions and API keys, use [modules/auth](modules-auth.md) instead; both can run in the same chain.

- [Security layers](../guides/security-layers.md#external-identity-providers-jwt): provider settings, claims to actors, key caching and outages.
