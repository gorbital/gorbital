package books_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/internal/modules/books/usecase"
)

// subscribe gives userID a plan ending at expires. A billing module would
// write this row; the test writes it directly.
func subscribe(t *testing.T, app *gorbitaltest.App, userID string, expires time.Time) {
	t.Helper()
	now := time.Now().UTC()
	_, err := app.App().Deps().DB.Exec(context.Background(),
		`INSERT INTO subscriptions (user_id, plan, expires_at, created_at, updated_at) VALUES ($1, 'monthly', $2, $3, $3)
		 ON CONFLICT (user_id) DO UPDATE SET expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
		userID, expires, now)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
}

// docs:start subscription-guard

// TestExportNeedsASubscription: the module's own guard refuses a reader
// without an active plan with the module's own code, and lets a subscriber
// through. Permission and subscription are different questions: both readers
// hold books.book.read.
func TestExportNeedsASubscription(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)

	// Never subscribed.
	ada.Get("/v1/books/export").AssertProblem(t, http.StatusForbidden, "active_subscription_refused")
	// Subscribed, but the plan ended yesterday.
	subscribe(t, app, "usr_ada", time.Now().Add(-24*time.Hour))
	ada.Get("/v1/books/export").AssertProblem(t, http.StatusForbidden, "active_subscription_refused")

	// Renewed: the guard runs before the handler, and lets it run.
	subscribe(t, app, "usr_ada", time.Now().Add(30*24*time.Hour))
	res := ada.Get("/v1/books/export")
	res.AssertStatus(t, http.StatusOK)
	var export struct {
		ExportedBy string `json:"exported_by"`
		Items      []book `json:"items"`
	}
	res.JSON(t, &export)
	if len(export.Items) != 1 {
		t.Errorf("exported %d books, want 1", len(export.Items))
	}

	// Another reader's subscription is not Ada's.
	app.As(gorbitaltest.User("usr_bob", usecase.PermRead)).
		Get("/v1/books/export").AssertProblem(t, http.StatusForbidden, "active_subscription_refused")
}

// docs:end subscription-guard
