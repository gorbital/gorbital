// Package delivery is the books module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/shelfie/internal/modules/books/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start routes

// Register adds the books routes to r. Every route requires a signed-in
// reader (deny by default) and the permission its guard names.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	books := r.Group("/v1/books", gorbital.Tags("Books"))

	gorbital.Post(books, "", h.createBook,
		gorbital.Summary("Add a book to your shelf"), gorbital.Status(http.StatusCreated),
		guard.Permission(usecase.PermWrite), guard.RateLimit(30, time.Minute))
	gorbital.Get(books, "", h.listBooks,
		gorbital.Summary("List your books"), guard.Permission(usecase.PermRead))
	gorbital.Get(books, "/{id}", h.getBook,
		gorbital.Summary("Get a book"), guard.Permission(usecase.PermRead))
	gorbital.Patch(books, "/{id}", h.updateBook,
		gorbital.Summary("Change a book"), guard.Permission(usecase.PermWrite))
	gorbital.Delete(books, "/{id}", h.deleteBook,
		gorbital.Summary("Remove a book"), gorbital.Status(http.StatusNoContent), guard.Permission(usecase.PermWrite))

	gorbital.Get(r.Group("/v1/shelves", gorbital.Tags("Books")), "", h.listShelves,
		gorbital.Summary("List your shelves"), guard.Permission(usecase.PermRead))
}

// docs:end routes
