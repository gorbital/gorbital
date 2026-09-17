package catalog

import (
	"net/http"

	gb "gorbital.dev/gorbital"

	"example.com/app/internal/modules/catalog/delivery"
)

func Module() gb.Module {
	return gb.Module{
		Name:       "books_catalog",
		Middleware: []func(http.Handler) http.Handler{delivery.Timing},
		Routes: func(r *gb.Router, d gb.Deps) {
			delivery.Register(r)
		},
	}
}
