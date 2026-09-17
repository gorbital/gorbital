package books_test

import (
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start client-version-refusals

// TestOldAppsAreToldToUpdate: RequireClientVersion on the books group reads
// X-App-Version and refuses anything below 2.0.0, before the sign-in check
// and before the guards.
func TestOldAppsAreToldToUpdate(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	ada.WithHeader("X-App-Version", "1.9.0").Get("/v1/books").
		AssertProblem(t, http.StatusUpgradeRequired, "app_outdated")
	// Middleware runs before the sign-in check, so a caller with no session
	// is told to update rather than to sign in.
	app.Client().WithHeader("X-App-Version", "1.9.0").Get("/v1/books").
		AssertProblem(t, http.StatusUpgradeRequired, "app_outdated")
	// Versions are compared as numbers: 10.0.0 is newer than 2.0.0.
	ada.WithHeader("X-App-Version", "10.0.0").Get("/v1/books").AssertStatus(t, http.StatusOK)
	ada.WithHeader("X-App-Version", "banana").Get("/v1/books").
		AssertProblem(t, http.StatusBadRequest, "invalid_app_version")
	// The web app sends no version, and is served.
	ada.Get("/v1/books").AssertStatus(t, http.StatusOK)
}

// docs:end client-version-refusals

// docs:start client-version-in-context

// TestTheExportNamesTheApp: the version RequireClientVersion computed
// reaches the handler through the request's context.
func TestTheExportNamesTheApp(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	subscribe(t, app, "usr_ada", time.Now().Add(30*24*time.Hour))

	var export struct {
		ExportedBy string `json:"exported_by"`
	}
	ada.WithHeader("X-App-Version", "2.4.0").Get("/v1/books/export").JSON(t, &export)
	if export.ExportedBy != "shelfie/2.4.0" {
		t.Errorf("exported_by = %q, want %q", export.ExportedBy, "shelfie/2.4.0")
	}
	// No header, no value in the context, and the field is left out.
	var webApp struct {
		ExportedBy string `json:"exported_by"`
	}
	ada.Get("/v1/books/export").JSON(t, &webApp)
	if webApp.ExportedBy != "" {
		t.Errorf("exported_by without X-App-Version = %q, want it left out", webApp.ExportedBy)
	}
}

// docs:end client-version-in-context
