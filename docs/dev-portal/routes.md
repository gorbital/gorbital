# Routes

Every route the app registered, and a request builder that sends a request through the portal's proxy and shows the answer. It reads the dev console, so the app must run with it ([dev console](../guides/dev-console.md)).

![The Routes screen](screenshots/routes.png)

## What you see

| Panel | What it shows |
|---|---|
| Header | The route count, how many are secured, and a filter: All, OpenAPI (routes the document describes) or Handlers |
| Modules | The routes grouped by their OpenAPI tag (`Auth`, `Ops: audit`, `Projects`, `(untagged)`…) with a count each |
| The list | Every route with its method, path, operation ID and summary; a search over method, path, operation and summary. The count is the sidebar's badge |
| Request builder | Opens on a route ("Pick a route" until then): the path parameters, the query, headers, a JSON body, and the auth mode |
| Response | The status, the duration, the headers worth showing and the body |

Auth modes: none, or a bearer token you paste (a session token or an API key). The token "Act as user" hands over from the [Authentication](authentication.md) screen is read when the builder opens; the builder switches to it and says whom it acts as.

## What you can do

- Fill a route's parameters and send it. The request goes through `/_portal/app/…`, so it reaches the app from `127.0.0.1`; a request to `/_dev/` or `/ops/` that carries no `Authorization` header gets the dev console token added by `orb dev`.
- Paste a bearer token to call `/v1/` as a user or an API key.
- Act as a user: "Use in Routes" on an account stores an impersonation token in this tab's `sessionStorage` (never in the URL) and opens the builder with it.

A request sent from the builder does whatever the route does: a `DELETE` deletes. Nothing is confirmed.

## Where it comes from

`GET /_portal/app/_dev/routes` and the proxy for the request itself. [ADR-0065](../adr/0065-local-dev-console-apis.md) decided the console; [ADR-0066](../adr/0066-dev-portal.md) the proxy and the development operator.

## Notes

- The "none" auth mode sends no header, but the proxy still adds the dev operator on `/ops/` and `/_dev/`. A pasted bearer token is the way to test those paths as somebody else.
- The SQL spans of a request aren't shown in the builder; the request's records are on [Logs](requests-and-logs.md#logs), with "All logs for this request".
- Impersonation only works while the app runs with the dev console; elsewhere it answers 403 `impersonation_off`.
