// Package books is Shelfie's books module: a reader's shelf of books, in
// four layers (domain, usecase, repository, delivery) with one file per
// operation in each.
package books

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"

	"example.com/shelfie/internal/modules/books/delivery"
	"example.com/shelfie/internal/modules/books/domain"
	"example.com/shelfie/internal/modules/books/repository"
	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start module

// Module returns the books module. main.go adds it with every other module
// through modules.All.
func Module() gorbital.Module {
	// Declared in Settings and Flags, before the stores exist, and used in
	// Routes: no lookup by key.
	var shelfLimit *settings.Setting[int]
	return gorbital.Module{
		Name: "books",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrBookNotFound, Status: http.StatusNotFound, Code: "book_not_found", Detail: "no book on your shelf has this ID"},
			{Err: domain.ErrTitleRequired, Status: http.StatusUnprocessableEntity, Code: "title_required", Detail: "a book needs a title of up to 300 characters"},
			{Err: domain.ErrAuthorTooLong, Status: http.StatusUnprocessableEntity, Code: "author_too_long", Detail: "the author is at most 200 characters"},
			{Err: domain.ErrInvalidISBN, Status: http.StatusUnprocessableEntity, Code: "invalid_isbn", Detail: "the ISBN must be an ISBN-10 or ISBN-13"},
			{Err: domain.ErrInvalidStatus, Status: http.StatusUnprocessableEntity, Code: "invalid_status", Detail: "status is want_to_read, reading or read"},
			{Err: domain.ErrISBNTaken, Status: http.StatusConflict, Code: "isbn_taken", Detail: "a book with this ISBN is already on your shelf"},
			{Err: domain.ErrShelfFull, Status: http.StatusConflict, Code: "shelf_full", Detail: "your shelf holds as many books as allowed; remove one first"},
		},
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the books on your shelf", Roles: []string{"user"}},
			{Name: usecase.PermWrite, Description: "Add, change and remove the books on your shelf", Roles: []string{"user"}},
		},
		// docs:start settings-and-flags
		Settings: func(r *settings.Registry) {
			shelfLimit = settings.Int(r, "books.shelf_limit", 1000,
				settings.Describe("How many books one reader's shelf holds. Adding more answers 409 shelf_full."),
				settings.Group("books"),
				settings.Range(1, 100_000),
				settings.ReasonRequired(),
			)
		},
		Flags: func(r *flags.Registry) {
			flags.Bool(r, "books.reading_goals",
				flags.Describe("Shows yearly reading goals in the web and mobile apps, which read it from GET /v1/flags."),
				flags.Group("books"),
				flags.Client(),
			)
		},
		// docs:end settings-and-flags
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the
			// service is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, shelfLimit)
			delivery.Register(r, svc)
		},
	}
}

// docs:end module
