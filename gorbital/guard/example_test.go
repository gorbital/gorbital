package guard_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

type catalogInput struct {
	ID string `path:"id"`
}

type catalogEntry struct {
	Body struct {
		Title string `json:"title"`
	}
}

func catalogBook(context.Context, *catalogInput) (*catalogEntry, error) {
	out := &catalogEntry{}
	out.Body.Title = "Dune"
	return out, nil
}

func ExamplePublic() {
	mux := http.NewServeMux()
	api := openapi.New(mux, "Shelfie", "1.0.0", openapi.WithBearerAuth("Session token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	openapi.InstallErrors(mapper)

	err = gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name: "catalog",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			public := r.Group("/v1/catalog", guard.Public())
			gorbital.Get(public, "/{id}", catalogBook)
			gorbital.Get(r, "/v1/wishlist/{id}", catalogBook)
		},
	})
	if err != nil {
		panic(err)
	}

	for _, target := range []string{"/v1/catalog/bok_1", "/v1/wishlist/bok_1"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		fmt.Println(target, rec.Code)
	}
	// Output:
	// /v1/catalog/bok_1 200
	// /v1/wishlist/bok_1 401
}
