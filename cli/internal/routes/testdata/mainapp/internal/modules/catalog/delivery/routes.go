package delivery

import (
	"context"
	"net/http"

	gb "gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
)

type handlers struct{}

type other struct{}

func Register(r *gb.Router) {
	h := &handlers{}
	v1 := r.Group("/v1", gb.Use(requireVersion("2.4.0")))
	books := v1.Group("/books", gb.Tags("Books"))

	gb.Get(books, "/{id}", h.get, gb.Use(Timing, cacheFor(60)), guard.Permission("catalog.book.read"))
	gb.Post(books, "", h.create, gb.OperationID("catalog-create"))
	gb.Get(r.Group("/v1/public", guard.Public()), "/{id}", h.get)
	gb.Delete(r, "/v1/books/{id}", deleteBook)
	gb.Get(r, "/v1/ping", func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	gb.Get(r, pathFor("x"), h.get)
}

func (h *handlers) get(context.Context, *struct{}) (*struct{}, error)    { return nil, nil }
func (h *handlers) create(context.Context, *struct{}) (*struct{}, error) { return nil, nil }
func (other) get()                                                       {}

func deleteBook(context.Context, *struct{}) (*struct{}, error) { return nil, nil }

func Timing(next http.Handler) http.Handler                 { return next }
func requireVersion(string) func(http.Handler) http.Handler { return Timing }
func cacheFor(int) func(http.Handler) http.Handler          { return Timing }
func pathFor(s string) string                               { return s }
