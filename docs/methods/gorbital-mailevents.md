# gorbital/mailevents

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/mailevents"
```

Package mailevents is the mail events module: POST /v1/webhooks/resend receives Resend's signed bounce and complaint webhooks and puts the addresses that bounced permanently or complained on the suppression list, which the mail worker checks before every send (ADR-0062). Operators list and remove suppressions through the operations API (gorbital.dev/gorbital/opshttp).

The webhook is on when RESEND\_WEBHOOK\_SECRET is set, and answers 404 webhook\_not\_found otherwise. Its path, operation ID, headers, error codes and audit action (mail.suppression.added) are those of the module a v0.1 app generated, and are public API (ADR-0015).

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Functions: [`Module`](#Module)

## Functions

<a id="Module"></a>

### func Module

```go
func Module() gorbital.Module
```

Module returns the mail events module. Its name is "mailevents". gorbital.New fails with a configuration error when RESEND\_WEBHOOK\_SECRET is set but isn't a Resend signing secret (whsec\_…).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
// cmd/api/main.go of an app receiving Resend's bounces and complaints;
// RESEND_WEBHOOK_SECRET turns the webhook on.
main := func() {
	gorbital.Main(gorbital.WithModules(mailevents.Module()))
}
_ = main

mux := http.NewServeMux()
api := openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token"))
mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
if err != nil {
	panic(err)
}
openapi.InstallErrors(mapper)
if err := gorbital.Mount(api, mapper, gorbital.Deps{}, mailevents.Module()); err != nil {
	panic(err)
}
// The webhook is public; without a secret it answers 404.
rec := httptest.NewRecorder()
mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/webhooks/resend", strings.NewReader(`{}`)))
fmt.Println(rec.Code, strings.Contains(rec.Body.String(), "webhook_not_found"))
```

Output:

```text
404 true
```
