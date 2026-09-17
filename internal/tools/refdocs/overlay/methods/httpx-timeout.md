## When to use

Apps on `gorbital.Main` have it already, as the `Timeout` step of the [middleware stack](../guides/middleware-stack.md) (`APP_REQUEST_TIMEOUT`), and shorten it per route with `gorbital.Timeout`. Add [New](#New) yourself to any other `net/http` chain, after `httpx.Recover` and `httpx.RequestID`, so slow queries end with a 503 instead of holding connections.

- [Security layers](../guides/security-layers.md#request-timeout): what happens at the deadline, streaming, and what it doesn't do.
- [ADR-0085](../adr/0085-security-layers.md): the design, and why it is a package of its own.
