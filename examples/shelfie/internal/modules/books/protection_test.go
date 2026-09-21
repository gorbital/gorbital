package books_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start deny-by-default

// TestRoutesDenyByDefault: no books route is public, so a caller without a
// session is refused before the request body is read, and a signed-in reader
// still needs the permission the route's guard names.
func TestRoutesDenyByDefault(t *testing.T) {
	app := newApp(t)

	// Nobody signed in. The body is invalid, and nobody looked: the sign-in
	// check runs before Huma parses it.
	anonymous := app.Client()
	anonymous.Get("/v1/books").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	anonymous.Post("/v1/books", map[string]string{"nonsense": ""}).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	// Signed in, holding books.book.read only: reading is allowed, writing
	// is 403, and the reader learns nothing about the route beyond that.
	reader := app.As(gorbitaltest.User("usr_ada", usecase.PermRead))
	reader.Get("/v1/books").AssertStatus(t, http.StatusOK)
	reader.Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	reader.Delete("/v1/books/bok_whatever").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end deny-by-default

// docs:start rate-limit

// TestAddingBooksIsRateLimited: guard.RateLimit(30, time.Minute) on
// POST /v1/books lets a burst of 30 through and refuses the 31st, per
// caller.
func TestAddingBooksIsRateLimited(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	for i := range 30 {
		ada.Post("/v1/books", map[string]string{"title": fmt.Sprintf("Book %d", i)}).AssertStatus(t, http.StatusCreated)
	}
	res := ada.Post("/v1/books", map[string]string{"title": "One too many"})
	res.AssertProblem(t, http.StatusTooManyRequests, "rate_limited")
	if res.Header.Get("Retry-After") == "" {
		t.Error("a rate-limited response has no Retry-After header")
	}

	// The budget is the caller's, not the route's: another reader still
	// adds books, and Ada still reads hers.
	app.As(gorbitaltest.User("usr_bob", usecase.PermWrite)).
		Post("/v1/books", map[string]string{"title": "Emma"}).AssertStatus(t, http.StatusCreated)
	ada.Get("/v1/books").AssertStatus(t, http.StatusOK)
}

// docs:end rate-limit

// docs:start recent-reauth

// TestEmptyingAShelfNeedsARecentSignIn: guard.RecentReauth on
// DELETE /v1/books refuses a session that signed in too long ago, and an API
// key, which has no session at all.
func TestEmptyingAShelfNeedsARecentSignIn(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)

	// The same reader, holding the same permission, on a session that
	// signed in an hour ago and verified no second factor since.
	stale := gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite)
	stale.SignedInAt = time.Now().Add(-time.Hour)
	stale.MFAVerified, stale.MFAVerifiedAt = false, time.Time{}
	app.As(stale).Delete("/v1/books").AssertProblem(t, http.StatusForbidden, "reauthentication_required")
	app.As(gorbitaltest.APIKey("usr_ada", usecase.PermWrite)).
		Delete("/v1/books").AssertProblem(t, http.StatusForbidden, "session_required")

	// Nothing was removed; a session that signed in just now may.
	var list struct{ Items []book }
	ada.Get("/v1/books").JSON(t, &list)
	if len(list.Items) != 1 {
		t.Fatalf("books after two refused deletes = %d, want 1", len(list.Items))
	}
	res := ada.Delete("/v1/books")
	res.AssertStatus(t, http.StatusOK)
	var emptied struct {
		Removed int `json:"removed"`
	}
	res.JSON(t, &emptied)
	if emptied.Removed != 1 {
		t.Errorf("removed = %d, want 1", emptied.Removed)
	}
}

// docs:end recent-reauth
