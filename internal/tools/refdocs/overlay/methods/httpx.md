## When to use

Every app uses httpx: generated apps build the server and the middleware chain in `internal/app/routes.go`, and map errors to problem+json responses with one [Mapper](#Mapper). Reach for it directly when you add a middleware, map a new error to a status and code, or write a problem response from a handler.

- [Life of a request](../guides/request-lifecycle.md): the middleware chain in order, and what each step does.
- [Error handling](../guides/error-handling.md): errors, mappings and problem codes.
- [Security layers](../guides/security-layers.md): request timeouts with [Timeout](#Timeout) and network restrictions with [IPFilter](#IPFilter).
