package mailevents_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

func ExampleModule() {
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
	// Output:
	// 404 true
}
