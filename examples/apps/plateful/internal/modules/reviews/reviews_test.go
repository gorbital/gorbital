package reviews_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/modules/postgres"

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	orgshttp "example.com/plateful/internal/modules/orgs"
	"example.com/plateful/internal/modules/restaurants"
	"example.com/plateful/internal/modules/reviews"
	"example.com/plateful/internal/modules/reviews/usecase"
)

// These tests drive the reviews routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts:
// customers are sign-in's users, belonging to no organisation that matters
// here.
//
// The restaurant and the order rows are inserted with SQL rather than
// through the API. The orders module writes them in the real flow — a
// customer places an order, the restaurant accepts it, a courier delivers it
// — but a module never imports another module's layers, and a test app here
// builds only the modules this one needs. What matters to the reviews module
// is four columns of one order row, which is exactly what these helpers set.

// newApp builds the app with sign-in, organisations, the restaurants module
// (whose table the orders rows reference) and the reviews module.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), restaurants.Module(), reviews.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets.
func signUp(t *testing.T, app *gorbitaltest.App, email string) (*gorbitaltest.Client, string, string) {
	t.Helper()
	client, userID := app.SignUp(t, email)
	var orgs struct {
		Items []struct {
			ID       string `json:"id"`
			Personal bool   `json:"personal"`
		} `json:"items"`
	}
	client.Get("/v1/orgs").JSON(t, &orgs)
	for _, org := range orgs.Items {
		if org.Personal {
			return client, userID, org.ID
		}
	}
	t.Fatalf("%s has no personal workspace", email)
	return nil, "", ""
}

// seedDB returns the pool and a context that isn't limited to one
// organisation, for preparing rows the way another module would write them.
func seedDB(t *testing.T, app *gorbitaltest.App) (*pgxpool.Pool, context.Context) {
	t.Helper()
	return app.App().Deps().DB, postgres.WithoutRowLevelSecurity(context.Background(), "test: seed another module's rows")
}

// seedRestaurant makes orgID an open restaurant called name, as the
// restaurants module would, and returns its ID: the organisation's.
func seedRestaurant(t *testing.T, app *gorbitaltest.App, orgID, name string) string {
	t.Helper()
	db, ctx := seedDB(t, app)
	_, err := db.Exec(ctx, `
		UPDATE orgs SET name = $2, address = 'Example address', status = 'open', profile_created_at = now()
		WHERE id = $1`, orgID, name)
	if err != nil {
		t.Fatalf("seed restaurant: %v", err)
	}
	return orgID
}

// seedOrder inserts an order of the restaurant, placed by customerID and in
// status, as the orders module would.
func seedOrder(t *testing.T, app *gorbitaltest.App, restaurantID, customerID, status, suffix string) string {
	t.Helper()
	db, ctx := seedDB(t, app)
	id := "ord_" + suffix
	_, err := db.Exec(ctx, `
		INSERT INTO orders (id, org_id, customer_id, status, address, total_minor,
		                    placed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Example address', 1250, now(), now(), now())`,
		id, restaurantID, customerID, status)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}
	return id
}

// apiReview is a review as the API returns it.
type apiReview struct {
	ID        string    `json:"id"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// apiReviewPage is a page of reviews with what they add up to.
type apiReviewPage struct {
	Items         []apiReview `json:"items"`
	NextCursor    string      `json:"next_cursor"`
	ReviewCount   int         `json:"review_count"`
	AverageRating float64     `json:"average_rating"`
}

func reviewPath(orderID string) string { return "/v1/orders/" + orderID + "/review" }
func listPath(restaurantID string) string {
	return "/v1/restaurants/" + restaurantID + "/reviews"
}

// TestWriteReviewMovesTheAggregate checks the write and the running total it
// keeps in the same transaction: two reviews of the same restaurant, and an
// average that is exactly their mean.
func TestWriteReviewMovesTheAggregate(t *testing.T) {
	app := newApp(t)
	_, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	ada, adaID, _ := signUp(t, app, "ada@example.com")
	bob, bobID, _ := signUp(t, app, "bob@example.com")

	// Before anybody reviews it, the restaurant has no rating row at all,
	// and the list says so rather than failing.
	var empty apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &empty)
	if len(empty.Items) != 0 || empty.ReviewCount != 0 || empty.AverageRating != 0 {
		t.Errorf("list of an unreviewed restaurant = %+v, want nothing", empty)
	}

	adaOrder := seedOrder(t, app, restaurant, adaID, "delivered", "ada")
	res := ada.Post(reviewPath(adaOrder), map[string]any{"rating": 5, "comment": "  Faultless  "})
	res.AssertStatus(t, http.StatusCreated)
	var written apiReview
	res.JSON(t, &written)
	if written.Rating != 5 || written.Comment != "Faultless" || written.Version != 1 {
		t.Errorf("written = %+v, want five stars, the comment trimmed and version 1", written)
	}

	var one apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &one)
	if one.ReviewCount != 1 || one.AverageRating != 5 {
		t.Errorf("after one review = %d reviews averaging %v, want 1 averaging 5", one.ReviewCount, one.AverageRating)
	}

	bobOrder := seedOrder(t, app, restaurant, bobID, "delivered", "bob")
	bob.Post(reviewPath(bobOrder), map[string]any{"rating": 2, "comment": "Cold"}).AssertStatus(t, http.StatusCreated)

	var two apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &two)
	if two.ReviewCount != 2 || two.AverageRating != 3.5 {
		t.Errorf("after two reviews = %d averaging %v, want 2 averaging 3.5", two.ReviewCount, two.AverageRating)
	}
	if len(two.Items) != 2 || two.Items[0].Comment != "Cold" {
		t.Errorf("list = %+v, want both reviews newest first", two.Items)
	}

	// Editing the stars moves the total with them, in the same transaction.
	ada.Patch("/v1/reviews/"+written.ID, map[string]any{"version": 1, "rating": 1, "comment": "Faultless"}).
		AssertStatus(t, http.StatusOK)
	var edited apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &edited)
	if edited.ReviewCount != 2 || edited.AverageRating != 1.5 {
		t.Errorf("after re-rating = %d averaging %v, want 2 averaging 1.5", edited.ReviewCount, edited.AverageRating)
	}
}

// TestTheEditWindowCloses checks the 24-hour window, and the version that
// guards an edit inside it.
func TestTheEditWindowCloses(t *testing.T) {
	app := newApp(t)
	_, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	ada, adaID, _ := signUp(t, app, "ada@example.com")
	order := seedOrder(t, app, restaurant, adaID, "delivered", "ada")

	var written apiReview
	ada.Post(reviewPath(order), map[string]any{"rating": 3, "comment": "Fine"}).JSON(t, &written)
	item := "/v1/reviews/" + written.ID

	ada.Patch(item, map[string]any{"version": 7, "rating": 4}).AssertProblem(t, http.StatusConflict, "review_version_conflict")
	res := ada.Patch(item, map[string]any{"version": 1, "rating": 4, "comment": "Better than I said"})
	res.AssertStatus(t, http.StatusOK)
	var edited apiReview
	res.JSON(t, &edited)
	if edited.Rating != 4 || edited.Version != 2 {
		t.Errorf("edited = %+v, want four stars and version 2", edited)
	}

	// Age the review by pushing created_at back, which is what the window is
	// measured from: editing it must not have bought another day.
	db, ctx := seedDB(t, app)
	if _, err := db.Exec(ctx, `UPDATE reviews SET created_at = created_at - interval '25 hours' WHERE id = $1`, written.ID); err != nil {
		t.Fatal(err)
	}
	ada.Patch(item, map[string]any{"version": 2, "rating": 1, "comment": "Changed my mind"}).
		AssertProblem(t, http.StatusConflict, "review_window_closed")
}

// TestOneReviewPerOrder checks the unique index behind review_exists.
func TestOneReviewPerOrder(t *testing.T) {
	app := newApp(t)
	_, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	ada, adaID, _ := signUp(t, app, "ada@example.com")
	order := seedOrder(t, app, restaurant, adaID, "delivered", "ada")

	ada.Post(reviewPath(order), map[string]any{"rating": 5}).AssertStatus(t, http.StatusCreated)
	ada.Post(reviewPath(order), map[string]any{"rating": 1, "comment": "On reflection"}).
		AssertProblem(t, http.StatusConflict, "review_exists")

	// The refused second review left the running total alone.
	var list apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &list)
	if list.ReviewCount != 1 || list.AverageRating != 5 {
		t.Errorf("after a refused second review = %d averaging %v, want 1 averaging 5", list.ReviewCount, list.AverageRating)
	}
}

// TestOnlyTheCustomerWhoOrderedMayReview checks the ownership rule that
// guard.Permission can't express: the order has to name the caller. Somebody
// else's order and an order that doesn't exist answer the same way, so order
// IDs can't be probed.
func TestOnlyTheCustomerWhoOrderedMayReview(t *testing.T) {
	app := newApp(t)
	owner, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	_, adaID, _ := signUp(t, app, "ada@example.com")
	bob, _, _ := signUp(t, app, "bob@example.com")
	adasOrder := seedOrder(t, app, restaurant, adaID, "delivered", "ada")

	bob.Post(reviewPath(adasOrder), map[string]any{"rating": 1, "comment": "Not mine"}).
		AssertProblem(t, http.StatusNotFound, "order_not_found")
	bob.Post(reviewPath("ord_nothing"), map[string]any{"rating": 1}).
		AssertProblem(t, http.StatusNotFound, "order_not_found")

	// The restaurant's own owner is no better placed than any other
	// stranger: the order is not theirs, whoever's organisation it belongs
	// to.
	owner.Post(reviewPath(adasOrder), map[string]any{"rating": 5, "comment": "Ours is excellent"}).
		AssertProblem(t, http.StatusNotFound, "order_not_found")

	// And nobody signed in at all is refused before any of that.
	app.Client().Post(reviewPath(adasOrder), map[string]any{"rating": 5}).
		AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
}

// TestAnOrderMustBeDeliveredFirst checks that only an order somebody has
// actually received can be rated.
func TestAnOrderMustBeDeliveredFirst(t *testing.T) {
	app := newApp(t)
	_, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	ada, adaID, _ := signUp(t, app, "ada@example.com")

	for _, status := range []string{"placed", "accepted", "preparing", "ready", "collected", "cancelled", "rejected"} {
		t.Run(status, func(t *testing.T) {
			order := seedOrder(t, app, restaurant, adaID, status, status)
			ada.Post(reviewPath(order), map[string]any{"rating": 5}).
				AssertProblem(t, http.StatusConflict, "order_not_delivered")
		})
	}
}

// TestThePublicListNeedsNoCredentials checks the app's one unauthenticated
// route, with its cursor, using a client that carries no principal at all.
func TestThePublicListNeedsNoCredentials(t *testing.T) {
	app := newApp(t)
	_, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	for i := range 3 {
		client, userID, _ := signUp(t, app, fmt.Sprintf("diner%d@example.com", i))
		order := seedOrder(t, app, restaurant, userID, "delivered", fmt.Sprintf("d%d", i))
		client.Post(reviewPath(order), map[string]any{"rating": i + 2, "comment": fmt.Sprintf("Visit %d", i)}).
			AssertStatus(t, http.StatusCreated)
	}

	anonymous := app.Client()
	var comments []string
	cursor := ""
	for range 4 {
		var p apiReviewPage
		anonymous.Get(listPath(restaurant)+"?limit=1&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
		if p.ReviewCount != 3 || p.AverageRating != 3 {
			t.Errorf("page reports %d reviews averaging %v, want 3 averaging 3", p.ReviewCount, p.AverageRating)
		}
		for _, item := range p.Items {
			comments = append(comments, item.Comment)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if want := 3; len(comments) != want {
		t.Errorf("paged comments = %v, want %d of them newest first", comments, want)
	} else if comments[0] != "Visit 2" || comments[2] != "Visit 0" {
		t.Errorf("paged comments = %v, want newest first", comments)
	}

	// A review says what was said, never who said it.
	var raw map[string]any
	anonymous.Get(listPath(restaurant)).JSON(t, &raw)
	items, _ := raw["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("list = %v, want items", raw)
	}
	first, _ := items[0].(map[string]any)
	for _, leaked := range []string{"customer_id", "customer", "order_id", "user_id"} {
		if _, ok := first[leaked]; ok {
			t.Errorf("public review carries %q: %v", leaked, first)
		}
	}
}

// TestOnlyPlatformStaffHideAReview is the product decision in a test: the
// restaurant a review is about has no route to hide it, and platform staff
// do. A hidden review leaves the public list and the average with it.
func TestOnlyPlatformStaffHideAReview(t *testing.T) {
	app := newApp(t)
	owner, _, org := signUp(t, app, "owner@example.com")
	restaurant := seedRestaurant(t, app, org, "website")
	ada, adaID, _ := signUp(t, app, "ada@example.com")
	bob, bobID, _ := signUp(t, app, "bob@example.com")

	adaOrder := seedOrder(t, app, restaurant, adaID, "delivered", "ada")
	bobOrder := seedOrder(t, app, restaurant, bobID, "delivered", "bob")
	var abusive apiReview
	ada.Post(reviewPath(adaOrder), map[string]any{"rating": 1, "comment": "Unrepeatable"}).JSON(t, &abusive)
	bob.Post(reviewPath(bobOrder), map[string]any{"rating": 5, "comment": "Wonderful"}).AssertStatus(t, http.StatusCreated)

	hide := "/v1/platform/reviews/" + abusive.ID + "/hide"
	reason := map[string]any{"reason": "Abusive language"}

	// There is no organisation-scoped route to hide a review, so the
	// restaurant's owner has nothing to call: the path doesn't exist.
	owner.Post("/v1/orgs/"+org+"/reviews/"+abusive.ID+"/hide", reason).AssertStatus(t, http.StatusNotFound)
	// And the platform's route refuses them, owner of the restaurant or not.
	owner.Post(hide, reason).AssertProblem(t, http.StatusForbidden, "forbidden")
	// So does the diner who wrote the other review.
	bob.Post(hide, reason).AssertProblem(t, http.StatusForbidden, "forbidden")

	var before apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &before)
	if before.ReviewCount != 2 || before.AverageRating != 3 {
		t.Fatalf("before moderation = %d averaging %v, want 2 averaging 3", before.ReviewCount, before.AverageRating)
	}

	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermModerate))
	staff.Post(hide, reason).AssertStatus(t, http.StatusOK)

	var after apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &after)
	if after.ReviewCount != 1 || after.AverageRating != 5 {
		t.Errorf("after moderation = %d averaging %v, want 1 averaging 5", after.ReviewCount, after.AverageRating)
	}
	if len(after.Items) != 1 || after.Items[0].Comment != "Wonderful" {
		t.Errorf("public list after moderation = %+v, want the hidden review gone", after.Items)
	}

	// Hiding it again counts it out once, not twice.
	staff.Post(hide, reason).AssertStatus(t, http.StatusOK)
	var again apiReviewPage
	app.Client().Get(listPath(restaurant)).JSON(t, &again)
	if again.ReviewCount != 1 {
		t.Errorf("after hiding twice = %d reviews, want 1", again.ReviewCount)
	}

	// The audit trail names the moderator, the reason and the restaurant's
	// organisation, which no actor here belongs to.
	db, ctx := seedDB(t, app)
	var action, actorID, orgID, metadata string
	err := db.QueryRow(ctx, `
		SELECT action, actor_id, coalesce(org_id, ''), coalesce(metadata::text, '')
		FROM audit_events WHERE resource_type = 'review' AND action = $1 ORDER BY id LIMIT 1`,
		usecase.ActionHidden).Scan(&action, &actorID, &orgID, &metadata)
	if err != nil {
		t.Fatal(err)
	}
	if actorID != "usr_staff" || orgID != org {
		t.Errorf("hidden event = %s by %s in %s, want the moderator and the restaurant's organisation %s", action, actorID, orgID, org)
	}
}
