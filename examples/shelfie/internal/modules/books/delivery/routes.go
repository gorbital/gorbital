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
// reader (deny by default) and the permission its guard names; the module's
// own middleware and guard are in this package (chapter 3).
func Register(r *gorbital.Router, svc *usecase.Service, subs *usecase.Subscriptions) {
	h := handlers{svc: svc}
	// docs:start books-group
	books := r.Group("/v1/books", gorbital.Tags("Books"),
		gorbital.Use(RequireClientVersion), gorbital.Errors(http.StatusUpgradeRequired))
	// docs:end books-group

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

	// docs:start empty-shelf-route
	// Nothing keeps a copy of a shelf, so a stolen session shouldn't reach
	// this: guard.RecentReauth asks the reader to sign in again first.
	gorbital.Delete(books, "", h.emptyShelf,
		gorbital.Summary("Empty your shelf"),
		gorbital.Description("Removes every book. Needs a session that signed in, or verified a second factor, in the last 10 minutes."),
		guard.Permission(usecase.PermWrite), guard.RecentReauth())
	// docs:end empty-shelf-route
	// docs:start export-route
	// Exporting a shelf is what Shelfie Plus is for: the module's own guard.
	gorbital.Get(books, "/export", h.exportBooks,
		gorbital.Summary("Export your shelf"),
		gorbital.Description("Every book on the shelf in one document, for readers on Shelfie Plus."),
		guard.Permission(usecase.PermRead), ActiveSubscription(subs))
	// docs:end export-route
}

// docs:end routes
