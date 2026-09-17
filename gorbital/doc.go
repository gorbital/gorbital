// Package gorbital composes gorbital's modules into an application
// (ADR-0081). A [Module] declares one feature of an app: its routes, error
// mappings, permissions, runtime settings and feature flags. [Declare] adds
// the declarations to the app's registries before their stores are built, and
// [Mount] registers the error mappings and routes on the app's API.
//
// Routes are declared with the generic functions [Get], [Post], [Put],
// [Patch] and [Delete] on a [Router], so handlers keep their typed input and
// output, and with them request validation and the OpenAPI document. Every
// route requires an authenticated actor unless it has guard.Public()
// (ADR-0082):
//
//	func Module() gorbital.Module {
//		return gorbital.Module{
//			Name:   "books",
//			Errors: []httpx.Mapping{{Err: ErrISBNTaken, Status: http.StatusConflict, Code: "isbn_taken"}},
//			Routes: func(r *gorbital.Router, d gorbital.Deps) {
//				h := &handler{db: d.DB, audit: d.Audit}
//				books := r.Group("/v1/books", gorbital.Tags("Books"))
//				gorbital.Post(books, "", h.createBook, gorbital.Status(http.StatusCreated))
//				gorbital.Get(books, "/{id}", h.getBook)
//			},
//		}
//	}
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package gorbital
