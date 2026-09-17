package flagshttp_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

func ExampleModule() {
	// cmd/api/main.go of an app whose clients read their feature flags.
	main := func() {
		gorbital.Main(gorbital.WithModules(flagshttp.Module()))
	}
	_ = main

	mux := http.NewServeMux()
	api := openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	openapi.InstallErrors(mapper)
	m := flagshttp.Module()
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, m); err != nil {
		panic(err)
	}
	fmt.Println(m.Permissions[0].Name, m.Permissions[0].Roles)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/flags", nil))
	fmt.Println(rec.Code)
	// Output:
	// flags.flag.read [user]
	// 401
}
