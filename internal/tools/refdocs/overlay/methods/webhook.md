## When to use

When your app receives webhooks: verify each request's signature before acting on it. In a gorbital app, pass the verifier to `guard.Webhook` on the route, which checks it before the body is parsed; elsewhere call [HMAC.Verify](#HMAC.Verify) with the raw body.

- [Security layers](../guides/security-layers.md#signed-webhooks): sender settings, secret rotation, replays and a custom verifier.
- [Guards and middleware](../guides/guards-and-middleware.md#webhooks).
