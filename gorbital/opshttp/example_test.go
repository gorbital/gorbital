package opshttp_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

func ExampleModule() {
	// cmd/api/main.go of an app with the operations API.
	main := func() {
		gorbital.Main(
			gorbital.WithModules(opshttp.Module()),
		)
	}
	_ = main

	// The module's routes, as the openapi command exports them.
	mux := http.NewServeMux()
	api := openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	openapi.InstallErrors(mapper)
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, opshttp.Module()); err != nil {
		panic(err)
	}
	fmt.Println(api.OpenAPI().Paths["/ops/settings"].Get.OperationID)

	// Every operation needs an authenticated actor holding its permission.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ops/settings", nil))
	fmt.Println(rec.Code)
	// Output:
	// ops-list-settings
	// 401
}

func ExampleOption() {
	var opts []opshttp.Option
	opts = append(opts, opshttp.MailProvider(opshttp.ProviderSMTP))
	_ = gorbital.WithModules(opshttp.Module(opts...))
}

func ExampleMailProvider() {
	// GET /ops/mail reports SMTP, for an app sending through its own SMTP
	// relay with gorbital.WithMailer.
	_ = gorbital.WithModules(opshttp.Module(opshttp.MailProvider(opshttp.ProviderSMTP)))
}
