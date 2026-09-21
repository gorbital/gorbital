package books_test

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/books"
	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start shelf-limit

// TestShelfLimitFromOps: an operator lowers books.shelf_limit through the
// operations API, and the next book over the limit is refused, without a
// restart.
func TestShelfLimitFromOps(t *testing.T) {
	app := gorbitaltest.New(t,
		gorbital.WithModules(opshttp.Module(), books.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	operator := app.As(gorbitaltest.User("usr_ops", "ops.settings.read", "ops.settings.write"))

	ada.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)

	// Changing the setting needs a reason, and the version read, so two
	// operators can't overwrite each other.
	operator.Put("/ops/settings/books.shelf_limit", map[string]any{"value": 1, "version": 0}).
		AssertProblem(t, http.StatusUnprocessableEntity, "setting_reason_required")
	operator.Put("/ops/settings/books.shelf_limit", map[string]any{"value": 1, "version": 0, "reason": "storage costs"}).
		AssertStatus(t, http.StatusOK)

	ada.Post("/v1/books", map[string]string{"title": "Emma"}).AssertProblem(t, http.StatusConflict, "shelf_full")
	// A reader can't change settings.
	ada.Put("/ops/settings/books.shelf_limit", map[string]any{"value": 5000, "version": 1, "reason": "more"}).
		AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end shelf-limit
