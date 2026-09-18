package orders_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/modules/settings"

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	"example.com/plateful/internal/modules/couriers"
	"example.com/plateful/internal/modules/menus"
	"example.com/plateful/internal/modules/notifications"
	"example.com/plateful/internal/modules/orders"
	"example.com/plateful/internal/modules/orders/repository"
	"example.com/plateful/internal/modules/orders/usecase"
	orgshttp "example.com/plateful/internal/modules/orgs"
	"example.com/plateful/internal/modules/restaurants"
)

// These tests drive the orders routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts: a
// restaurant's staff are members of its organisation, and the customer and
// the courier are accounts that belong to none.

// newApp builds an app with sign-in, organisations, /ops and the modules an
// order touches.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(restaurants.Module(), menus.Module(), couriers.Module(),
			notifications.Module(), orders.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// A kitchen is one restaurant, ready to take orders, with a menu.
type kitchen struct {
	staff        *gorbitaltest.Client
	orgID        string
	restaurantID string
	// margherita is unlimited; olives has three portions left.
	margherita, olives string
}

// openRestaurant signs a restaurateur up, gives them an organisation, opens
// the restaurant and writes a menu.
func openRestaurant(t *testing.T, app *gorbitaltest.App, email, name string) kitchen {
	t.Helper()
	staff, _ := app.SignUp(t, email)
	res := staff.Post("/v1/orgs", map[string]string{"name": name})
	res.AssertStatus(t, http.StatusCreated)
	var org struct {
		ID string `json:"id"`
	}
	res.JSON(t, &org)

	profile := map[string]any{
		"version": 0, "name": name, "address": "12 Market Street, Leeds", "cuisine": "Neapolitan",
		"opens_minute": 0, "closes_minute": 0, "delivery_radius_m": 3000, "status": "onboarding",
	}
	path := "/v1/orgs/" + org.ID + "/restaurant"
	res = staff.Put(path, profile)
	res.AssertStatus(t, http.StatusOK)
	var restaurant struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	res.JSON(t, &restaurant)
	profile["version"], profile["status"] = restaurant.Version, "open"
	staff.Put(path, profile).AssertStatus(t, http.StatusOK)

	k := kitchen{staff: staff, orgID: org.ID, restaurantID: restaurant.ID}
	k.margherita = addDish(t, staff, org.ID, "Margherita", 1250, nil)
	three := 3
	k.olives = addDish(t, staff, org.ID, "Olives", 400, &three)
	return k
}

// addDish puts one item on the menu and returns its ID. stock is nil for a
// dish the kitchen never runs out of.
func addDish(t *testing.T, staff *gorbitaltest.Client, orgID, name string, priceMinor int, stock *int) string {
	t.Helper()
	body := map[string]any{"section": "Pizza", "name": name, "price_minor": priceMinor}
	if stock != nil {
		body["stock"] = *stock
	}
	res := staff.Post("/v1/orgs/"+orgID+"/menu/items", body)
	res.AssertStatus(t, http.StatusCreated)
	var item struct {
		ID string `json:"id"`
	}
	res.JSON(t, &item)
	return item.ID
}

// apiOrder is an order as the API returns it.
type apiOrder struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	CustomerID string `json:"customer_id"`
	CourierID  string `json:"courier_id"`
	TotalMinor int64  `json:"total_minor"`
	Currency   string `json:"currency"`
	Lines      []struct {
		Name       string `json:"name"`
		PriceMinor int64  `json:"price_minor"`
		Quantity   int    `json:"quantity"`
	} `json:"lines"`
}

type apiOrderPage struct {
	Items      []apiOrder `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

// basket is an order for two margheritas and a portion of olives: £29.00.
func basket(k kitchen) map[string]any {
	return map[string]any{
		"address": "3 Kirkgate, Leeds",
		"items": []map[string]any{
			{"item_id": k.margherita, "quantity": 2},
			{"item_id": k.olives, "quantity": 1},
		},
	}
}

// simple is an order for two margheritas: £25.00, and nothing with limited
// stock, for the tests that place many orders.
func simple(k kitchen) map[string]any {
	return map[string]any{
		"address": "3 Kirkgate, Leeds",
		"items":   []map[string]any{{"item_id": k.margherita, "quantity": 2}},
	}
}

// place sends a basket and returns what the API answered.
func place(t *testing.T, customer *gorbitaltest.Client, k kitchen, body map[string]any) *gorbitaltest.Response {
	t.Helper()
	return customer.Post("/v1/restaurants/"+k.restaurantID+"/orders", body)
}

// docs:start test-authorise-payment

// authorisePayment writes the order's payment as the payments module's
// webhook would. Accepting an order reads order_payments.status, so a test
// of the orders module has to put a restaurant in a position to accept, and
// this is the smallest honest way: the row the other module owns, written
// directly, with the module that owns it left out of the test's app.
func authorisePayment(t *testing.T, app *gorbitaltest.App, k kitchen, order apiOrder, customerID string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := app.App().Deps().DB.Exec(context.Background(), `
		INSERT INTO order_payments (id, org_id, order_id, customer_id, amount_minor, currency,
		                            status, provider_ref, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'authorised', 'test', 1, $7, $7)`,
		"pay_"+order.ID, k.orgID, order.ID, customerID, order.TotalMinor, order.Currency, now)
	if err != nil {
		t.Fatal(err)
	}
}

// docs:end test-authorise-payment

// noReason is an empty body for the operations whose only field is an
// optional reason. Huma asks for a body whenever the input names one, so
// there is no way to leave it out entirely.
var noReason = map[string]any{}

// operator is a client holding one or more of the /ops permissions.
func operator(app *gorbitaltest.App, permissions ...string) *gorbitaltest.Client {
	return app.As(gorbitaltest.User("usr_operator", permissions...))
}

// setSetting changes a runtime setting through /ops, as an operator would.
func setSetting(t *testing.T, app *gorbitaltest.App, key string, value any) {
	t.Helper()
	ops := operator(app, "ops.settings.read", "ops.settings.write")
	var current struct {
		Version int64 `json:"version"`
	}
	ops.Get("/ops/settings/"+key).JSON(t, &current)
	ops.Put("/ops/settings/"+key, map[string]any{
		"value": value, "version": current.Version, "reason": "the test needs it",
	}).AssertStatus(t, http.StatusOK)
}

// setFlag replaces a feature flag's whole state through /ops.
func setFlag(t *testing.T, app *gorbitaltest.App, key string, state map[string]any) {
	t.Helper()
	ops := operator(app, "ops.flags.read", "ops.flags.write")
	var current struct {
		Version int64 `json:"version"`
	}
	ops.Get("/ops/flags/"+key).JSON(t, &current)
	ops.Put("/ops/flags/"+key, map[string]any{
		"state": state, "version": current.Version, "reason": "the test needs it",
	}).AssertStatus(t, http.StatusOK)
}

// docs:start test-happy-path

// TestAnOrderFromBasketToDoor walks the whole state machine with the three
// kinds of caller it needs: a customer places, a restaurant accepts and
// cooks, a courier collects and delivers. Each step is refused for anyone
// else, which is the point of walking it end to end.
func TestAnOrderFromBasketToDoor(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, dinerID := app.SignUp(t, "diner@example.com")

	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)
	if order.Status != "placed" || order.TotalMinor != 2900 || order.CustomerID != dinerID {
		t.Fatalf("placed = %+v, want a placed order of 2900 pence for the diner", order)
	}
	if len(order.Lines) != 2 || order.Lines[0].Name != "Margherita" || order.Lines[0].PriceMinor != 1250 {
		t.Fatalf("lines = %+v, want the dishes priced as the menu had them", order.Lines)
	}

	staffOrder := "/v1/orgs/" + k.orgID + "/orders/" + order.ID

	// The kitchen can't start until the money is authorised. That status is
	// the payments module's, and this is the rule that spans the two.
	k.staff.Post(staffOrder+"/accept", nil).AssertProblem(t, http.StatusConflict, "payment_not_authorised")
	authorisePayment(t, app, k, order, dinerID)
	k.staff.Post(staffOrder+"/accept", nil).AssertStatus(t, http.StatusOK)
	// Accepting twice is not accepting again: the state machine refuses it.
	k.staff.Post(staffOrder+"/accept", nil).AssertProblem(t, http.StatusConflict, "invalid_order_transition")

	k.staff.Post(staffOrder+"/preparing", nil).AssertStatus(t, http.StatusOK)
	k.staff.Post(staffOrder+"/ready", nil).AssertStatus(t, http.StatusOK)

	// A courier: a signed-in account with a courier profile and no
	// organisation anywhere.
	rider, _ := app.SignUp(t, "rider@example.com")
	courierID := registerCourier(t, rider)
	k.staff.Post(staffOrder+"/courier", map[string]string{"courier_id": courierID}).AssertStatus(t, http.StatusOK)

	rider.Post("/v1/orders/"+order.ID+"/collect", nil).AssertStatus(t, http.StatusOK)
	res = rider.Post("/v1/orders/"+order.ID+"/deliver", nil)
	res.AssertStatus(t, http.StatusOK)
	var delivered apiOrder
	res.JSON(t, &delivered)
	if delivered.Status != "delivered" {
		t.Fatalf("delivered = %+v, want delivered", delivered)
	}

	// Delivering frees the courier again, in the same transaction.
	var carrying string
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT active_order_id FROM couriers WHERE id = $1`, courierID).Scan(&carrying); err != nil {
		t.Fatal(err)
	}
	if carrying != "" {
		t.Errorf("the courier is still carrying %q after delivering", carrying)
	}
}

// docs:end test-happy-path

// registerCourier signs the client up as a courier and returns its ID.
func registerCourier(t *testing.T, client *gorbitaltest.Client) string {
	t.Helper()
	res := client.Post("/v1/couriers", map[string]any{
		"display_name": "Ada on a bicycle", "vehicle": "bicycle",
	})
	res.AssertStatus(t, http.StatusCreated)
	var courier struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	res.JSON(t, &courier)
	// A courier starts off duty; going on duty is its own change.
	client.Patch("/v1/couriers/me", map[string]any{"version": courier.Version, "available": true}).
		AssertStatus(t, http.StatusOK)
	return courier.ID
}

// docs:start test-three-shapes

// TestThreeWaysToReachOneOrder is the module's reason for existing. The same
// row is readable by a restaurant's staff, by the customer who placed it and
// by the courier carrying it — and by nobody else — and each of the three
// gets there a different way.
func TestThreeWaysToReachOneOrder(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	other := openRestaurant(t, app, "sara@example.com", "Sara's Kitchen")
	diner, dinerID := app.SignUp(t, "diner@example.com")
	stranger, _ := app.SignUp(t, "stranger@example.com")
	rider, _ := app.SignUp(t, "rider@example.com")
	otherRider, _ := app.SignUp(t, "other-rider@example.com")

	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)
	mine := "/v1/orders/" + order.ID

	// The restaurant's staff, through their organisation.
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders/"+order.ID).AssertStatus(t, http.StatusOK)
	// Another restaurant's staff can't even name this one's organisation.
	other.staff.Get("/v1/orgs/"+k.orgID+"/orders/"+order.ID).
		AssertProblem(t, http.StatusNotFound, "org_not_found")
	// And the order isn't in their own restaurant's orders either.
	other.staff.Get("/v1/orgs/"+other.orgID+"/orders/"+order.ID).
		AssertProblem(t, http.StatusNotFound, "order_not_found")

	// The customer, with no organisation at all.
	var got apiOrder
	diner.Get(mine).JSON(t, &got)
	if got.ID != order.ID || got.CustomerID != dinerID {
		t.Errorf("the customer's own order = %+v", got)
	}
	// Another signed-in account gets the same answer as for an order that
	// doesn't exist, so IDs can't be probed.
	stranger.Get(mine).AssertProblem(t, http.StatusNotFound, "order_not_found")
	app.Client().Get(mine).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	// The courier, once the order is theirs — and not before.
	courierID := registerCourier(t, rider)
	registerCourier(t, otherRider)
	rider.Get(mine).AssertProblem(t, http.StatusNotFound, "order_not_found")

	authorisePayment(t, app, k, order, dinerID)
	staffOrder := "/v1/orgs/" + k.orgID + "/orders/" + order.ID
	k.staff.Post(staffOrder+"/accept", nil).AssertStatus(t, http.StatusOK)
	k.staff.Post(staffOrder+"/courier", map[string]string{"courier_id": courierID}).AssertStatus(t, http.StatusOK)

	rider.Get(mine).AssertStatus(t, http.StatusOK)
	otherRider.Get(mine).AssertProblem(t, http.StatusNotFound, "order_not_found")
	// Nor can another courier move it along.
	otherRider.Post(mine+"/collect", nil).AssertProblem(t, http.StatusNotFound, "order_not_found")
	// An account with no courier profile isn't a courier at all.
	stranger.Post(mine+"/collect", nil).AssertProblem(t, http.StatusNotFound, "not_a_courier")
	// And the customer can't carry their own dinner.
	diner.Post(mine+"/collect", nil).AssertProblem(t, http.StatusNotFound, "not_a_courier")
}

// docs:end test-three-shapes

// docs:start test-one-transaction

// TestPlacingAnOrderIsOneTransaction: the order, its lines, the stock the
// dishes take and the job that tells the restaurant all land together.
// Nothing here is a second step that could fail on its own.
func TestPlacingAnOrderIsOneTransaction(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	registerEndpoint(t, k)
	diner, _ := app.SignUp(t, "diner@example.com")

	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)

	// The olives had three portions; the order took one.
	if left := stockOf(t, app, k.olives); left == nil || *left != 2 {
		t.Errorf("olives left = %v, want 2", left)
	}
	// And the restaurant was told, by a job written in the same
	// transaction. Workers don't run in tests, so the job row is what there
	// is to see.
	queued := app.Jobs(t, notifications.FanoutJob)
	if len(queued) != 1 {
		t.Fatalf("fanout jobs = %d, want 1", len(queued))
	}

	// Taking the last two portions works; a third order finds none.
	third := map[string]any{"address": "3 Kirkgate, Leeds", "items": []map[string]any{{"item_id": k.olives, "quantity": 2}}}
	place(t, diner, k, third).AssertStatus(t, http.StatusCreated)
	place(t, diner, k, third).AssertProblem(t, http.StatusConflict, "dish_out_of_stock")

	// The refused order left nothing behind: no row, no stock movement, no
	// job.
	if left := stockOf(t, app, k.olives); left == nil || *left != 0 {
		t.Errorf("olives left = %v, want 0", left)
	}
	if queued := app.Jobs(t, notifications.FanoutJob); len(queued) != 2 {
		t.Errorf("fanout jobs = %d, want 2: the refused order enqueued nothing", len(queued))
	}
	var placed int
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT count(*) FROM orders WHERE org_id = $1`, k.orgID).Scan(&placed); err != nil {
		t.Fatal(err)
	}
	if placed != 2 {
		t.Errorf("orders = %d, want the two that were accepted", placed)
	}
}

// docs:end test-one-transaction

// registerEndpoint gives the restaurant somewhere to be notified.
func registerEndpoint(t *testing.T, k kitchen) {
	t.Helper()
	k.staff.Post("/v1/orgs/"+k.orgID+"/notification-endpoints", map[string]string{
		"label": "Kitchen chat", "url": "https://hooks.example.com/services/T000/B000/xxxx",
	}).AssertStatus(t, http.StatusCreated)
}

// stockOf returns a dish's remaining stock, nil when it is unlimited.
func stockOf(t *testing.T, app *gorbitaltest.App, itemID string) *int {
	t.Helper()
	var stock *int
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT stock FROM menu_items WHERE id = $1`, itemID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	return stock
}

// docs:start test-guard-and-middleware

// TestTheRestaurantHasToBeOpen: the module's own guard refuses an order at a
// restaurant that isn't taking any, before the handler runs — and a platform
// suspension takes effect the moment it commits, with nothing to push
// anywhere.
func TestTheRestaurantHasToBeOpen(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, _ := app.SignUp(t, "diner@example.com")
	place(t, diner, k, basket(k)).AssertStatus(t, http.StatusCreated)

	staff := app.As(gorbitaltest.User("usr_staff", "restaurants.restaurant.suspend"))
	staff.Post("/v1/platform/restaurants/"+k.restaurantID+"/suspend",
		map[string]string{"reason": "selling food it doesn't have"}).AssertStatus(t, http.StatusOK)

	place(t, diner, k, basket(k)).AssertProblem(t, http.StatusConflict, "restaurant_not_accepting")
	// A restaurant that doesn't exist answers the same way as one that isn't
	// open to this caller.
	diner.Post("/v1/restaurants/org_nothing/orders", basket(k)).
		AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
}

// TestOrderingCanBePaused: the module's middleware stops every customer
// write while an operator has orders.ordering_paused on, and leaves reading
// alone. It runs before sign-in and the guards, so it costs nothing.
func TestOrderingCanBePaused(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, _ := app.SignUp(t, "diner@example.com")
	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)

	setSetting(t, app, "orders.ordering_paused", true)

	place(t, diner, k, basket(k)).AssertProblem(t, http.StatusServiceUnavailable, "ordering_paused")
	diner.Post("/v1/orders/"+order.ID+"/cancel", noReason).
		AssertProblem(t, http.StatusServiceUnavailable, "ordering_paused")
	// Reading is untouched, and so is the restaurant's own side.
	diner.Get("/v1/orders/"+order.ID).AssertStatus(t, http.StatusOK)
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders").AssertStatus(t, http.StatusOK)

	setSetting(t, app, "orders.ordering_paused", false)
	place(t, diner, k, basket(k)).AssertStatus(t, http.StatusCreated)
}

// docs:end test-guard-and-middleware

// docs:start test-settings-and-flags

// TestSettingsAndFlags: two runtime settings and two feature flags decide
// what this module does, and an operator changes all four in /ops without a
// deploy.
func TestSettingsAndFlags(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, _ := app.SignUp(t, "diner@example.com")

	// A kitchen that can only hold one order at a time.
	setSetting(t, app, "orders.max_open_per_restaurant", 1)
	place(t, diner, k, basket(k)).AssertStatus(t, http.StatusCreated)
	place(t, diner, k, basket(k)).AssertProblem(t, http.StatusConflict, "restaurant_busy")
	setSetting(t, app, "orders.max_open_per_restaurant", 50)

	// Scheduled ordering is a client-visible flag: the customer app reads it
	// from GET /v1/flags, and the API refuses a scheduled order while it is
	// off, so a client that ignores the flag gets the same answer as one
	// that reads it.
	var clientFlags struct {
		Flags map[string]bool `json:"flags"`
	}
	diner.Get("/v1/flags").JSON(t, &clientFlags)
	if on, ok := clientFlags.Flags["orders.scheduled_ordering"]; !ok || on {
		t.Fatalf("GET /v1/flags = %v, want orders.scheduled_ordering off", clientFlags.Flags)
	}
	if _, listed := clientFlags.Flags["orders.courier_auto_assign"]; listed {
		t.Error("a server-side flag is in GET /v1/flags; only flags.Client() ones belong there")
	}

	later := basket(k)
	later["scheduled_for"] = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	place(t, diner, k, later).AssertProblem(t, http.StatusUnprocessableEntity, "scheduling_unavailable")

	setFlag(t, app, "orders.scheduled_ordering", map[string]any{"enabled": true, "default": true})
	diner.Get("/v1/flags").JSON(t, &clientFlags)
	if !clientFlags.Flags["orders.scheduled_ordering"] {
		t.Error("the flag is on but the customer app is still told it is off")
	}
	place(t, diner, k, later).AssertStatus(t, http.StatusCreated)
}

// docs:end test-settings-and-flags

// docs:start test-flag-targeting

// TestCourierAutoAssignRollout: the server-side flag, rolled out the way
// flags are actually rolled out — to one organisation first.
//
// The bucket subject is the organisation when the caller acts in one, so a
// percentage moves whole restaurants at a time and a restaurant never sees
// the feature flicker between two orders. Bruno's restaurant is allowed
// explicitly; Sara's is left out, and gets no courier.
func TestCourierAutoAssignRollout(t *testing.T) {
	app := newApp(t)
	bruno := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	sara := openRestaurant(t, app, "sara@example.com", "Sara's Kitchen")
	diner, dinerID := app.SignUp(t, "diner@example.com")
	rider, _ := app.SignUp(t, "rider@example.com")
	courierID := registerCourier(t, rider)

	setFlag(t, app, "orders.courier_auto_assign", map[string]any{
		"enabled": true, "default": false,
		"orgs": map[string]any{"allow": []string{bruno.orgID}},
	})

	for _, tt := range []struct {
		name string
		k    kitchen
		want string
	}{
		{"in the rollout", bruno, courierID},
		{"outside it", sara, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := place(t, diner, tt.k, basket(tt.k))
			res.AssertStatus(t, http.StatusCreated)
			var order apiOrder
			res.JSON(t, &order)
			authorisePayment(t, app, tt.k, order, dinerID)
			path := "/v1/orgs/" + tt.k.orgID + "/orders/" + order.ID
			tt.k.staff.Post(path+"/accept", nil).AssertStatus(t, http.StatusOK)
			tt.k.staff.Post(path+"/preparing", nil).AssertStatus(t, http.StatusOK)
			res = tt.k.staff.Post(path+"/ready", nil)
			res.AssertStatus(t, http.StatusOK)
			var ready apiOrder
			res.JSON(t, &ready)
			if ready.CourierID != tt.want {
				t.Errorf("courier after ready = %q, want %q", ready.CourierID, tt.want)
			}
			// Free the courier again for the next case.
			if ready.CourierID != "" {
				rider.Post("/v1/orders/"+order.ID+"/collect", nil).AssertStatus(t, http.StatusOK)
				rider.Post("/v1/orders/"+order.ID+"/deliver", nil).AssertStatus(t, http.StatusOK)
			}
		})
	}
}

// docs:end test-flag-targeting

// docs:start test-listing

// TestListingOrders: pagination, filtering and sorting on the restaurant's
// list, and the two lists a caller with no organisation gets.
func TestListingOrders(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	other := openRestaurant(t, app, "sara@example.com", "Sara's Kitchen")
	diner, dinerID := app.SignUp(t, "diner@example.com")
	stranger, _ := app.SignUp(t, "stranger@example.com")

	var first apiOrder
	for i := range 3 {
		res := place(t, diner, k, simple(k))
		res.AssertStatus(t, http.StatusCreated)
		if i == 0 {
			res.JSON(t, &first)
		}
	}
	place(t, stranger, k, simple(k)).AssertStatus(t, http.StatusCreated)
	place(t, stranger, other, simple(other)).AssertStatus(t, http.StatusCreated)

	// The restaurant sees its own four and no more, a page at a time.
	seen := map[string]bool{}
	cursor := ""
	for range 5 {
		var p apiOrderPage
		k.staff.Get("/v1/orgs/"+k.orgID+"/orders?limit=2&cursor="+cursor).JSON(t, &p)
		for _, o := range p.Items {
			seen[o.ID] = true
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != 4 {
		t.Errorf("the restaurant's orders = %d, want 4", len(seen))
	}

	// A cursor belongs to the sort it came from, and to nothing else.
	var page1 apiOrderPage
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders?limit=1").JSON(t, &page1)
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders?sort=total_minor&cursor="+page1.NextCursor).
		AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders?sort=customer_id").
		AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	// docs:start test-cursor-is-not-authorisation
	// A cursor says where a page ended and nothing about who may read the
	// next one: the owner filter is in every query. Sara's restaurant
	// borrowing Bruno's cursor still sees only Sara's orders.
	var sarasPage apiOrderPage
	other.staff.Get("/v1/orgs/"+other.orgID+"/orders?cursor="+page1.NextCursor).JSON(t, &sarasPage)
	for _, o := range sarasPage.Items {
		if o.ID == first.ID {
			t.Fatal("a cursor from another restaurant leaked its orders")
		}
	}
	// docs:end test-cursor-is-not-authorisation

	// A filter narrows what the caller may already see.
	authorisePayment(t, app, k, first, dinerID)
	k.staff.Post("/v1/orgs/"+k.orgID+"/orders/"+first.ID+"/accept", nil).AssertStatus(t, http.StatusOK)
	var accepted apiOrderPage
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders?status=accepted").JSON(t, &accepted)
	if len(accepted.Items) != 1 || accepted.Items[0].ID != first.ID {
		t.Errorf("filtered by status = %+v, want the one accepted order", accepted.Items)
	}
	var future apiOrderPage
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders?from="+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)).JSON(t, &future)
	if len(future.Items) != 0 {
		t.Errorf("orders placed in the next hour = %+v, want none", future.Items)
	}

	// The customer's own list crosses restaurants and stops at their own
	// orders.
	var mine apiOrderPage
	diner.Get("/v1/orders").JSON(t, &mine)
	if len(mine.Items) != 3 {
		t.Errorf("the customer's list = %d orders, want their 3", len(mine.Items))
	}
	for _, o := range mine.Items {
		if o.CustomerID != dinerID {
			t.Fatalf("the customer's list carries somebody else's order: %+v", o)
		}
	}
}

// docs:end test-listing

// docs:start test-summary

// TestDailySummary: the numbers come back from one query, and they are the
// numbers, not an approximation.
func TestDailySummary(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, dinerID := app.SignUp(t, "diner@example.com")
	rider, _ := app.SignUp(t, "rider@example.com")
	courierID := registerCourier(t, rider)

	// Two orders: one all the way to the door, one rejected.
	var delivered, rejected apiOrder
	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	res.JSON(t, &delivered)
	res = place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	res.JSON(t, &rejected)

	authorisePayment(t, app, k, delivered, dinerID)
	path := "/v1/orgs/" + k.orgID + "/orders/" + delivered.ID
	for _, step := range []string{"/accept", "/preparing", "/ready"} {
		k.staff.Post(path+step, nil).AssertStatus(t, http.StatusOK)
	}
	k.staff.Post(path+"/courier", map[string]string{"courier_id": courierID}).AssertStatus(t, http.StatusOK)
	rider.Post("/v1/orders/"+delivered.ID+"/collect", nil).AssertStatus(t, http.StatusOK)
	rider.Post("/v1/orders/"+delivered.ID+"/deliver", nil).AssertStatus(t, http.StatusOK)
	k.staff.Post("/v1/orgs/"+k.orgID+"/orders/"+rejected.ID+"/reject",
		map[string]string{"reason": "out of dough"}).AssertStatus(t, http.StatusOK)

	var summary struct {
		Orders       int            `json:"orders"`
		ByStatus     map[string]int `json:"by_status"`
		RevenueMinor int64          `json:"revenue_minor"`
		Currency     string         `json:"currency"`
		Busiest      int            `json:"busiest_orders"`
	}
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders/summary").JSON(t, &summary)
	if summary.Orders != 2 || summary.ByStatus["delivered"] != 1 || summary.ByStatus["rejected"] != 1 {
		t.Errorf("summary = %+v, want two orders, one delivered and one rejected", summary)
	}
	if summary.RevenueMinor != 2900 || summary.Currency != "GBP" {
		t.Errorf("revenue = %d %s, want 2900 GBP: only the delivered order counts", summary.RevenueMinor, summary.Currency)
	}
	if summary.Busiest != 2 {
		t.Errorf("busiest hour = %d orders, want 2", summary.Busiest)
	}

	// A period that isn't one is refused rather than answered with zeroes.
	now := time.Now().UTC().Format(time.RFC3339)
	k.staff.Get("/v1/orgs/"+k.orgID+"/orders/summary?from="+now+"&to="+now).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	// And another restaurant can't ask about this one's day.
	openRestaurant(t, app, "sara@example.com", "Sara's Kitchen").staff.
		Get("/v1/orgs/"+k.orgID+"/orders/summary").AssertProblem(t, http.StatusNotFound, "org_not_found")
}

// docs:end test-summary

// docs:start test-late-sweep

// TestLateSweep drives the orders_late_sweep job's worker directly. Workers
// don't run in tests, so the way to test one is to build it and call Work:
// the notifier it is given here enqueues through the app's real job client,
// so what it produced can be read back with App.Jobs.
func TestLateSweep(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	registerEndpoint(t, k)
	diner, dinerID := app.SignUp(t, "diner@example.com")

	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)
	authorisePayment(t, app, k, order, dinerID)
	k.staff.Post("/v1/orgs/"+k.orgID+"/orders/"+order.ID+"/accept", nil).AssertStatus(t, http.StatusOK)

	deps := app.App().Deps()
	svc := usecase.NewService(repository.NewStore(deps.DB), nil, deps.Audit, nil,
		usecase.Config{LateAfter: lateAfterSetting(t, app)})
	worker := usecase.NewLateSweepWorker(svc, notifications.With(deps.Jobs), nil)

	before := len(app.Jobs(t, notifications.FanoutJob))
	if err := worker.Work(context.Background(), &river.Job[usecase.LateSweepArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(app.Jobs(t, notifications.FanoutJob)); got != before {
		t.Fatalf("fanout jobs after a sweep with nothing late = %d, want %d", got, before)
	}

	// Put the order half an hour in the past, which is what
	// orders.late_after allows by default.
	if _, err := deps.DB.Exec(context.Background(),
		`UPDATE orders SET accepted_at = accepted_at - interval '45 minutes' WHERE id = $1`, order.ID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Work(context.Background(), &river.Job[usecase.LateSweepArgs]{
		JobRow: &rivertype.JobRow{ID: 2, Attempt: 1, MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(app.Jobs(t, notifications.FanoutJob)); got != before+1 {
		t.Errorf("fanout jobs after a sweep with one late order = %d, want %d", got, before+1)
	}

	// The use case the job exists for can be called on its own, which is the
	// point of it being a use case and not a method on the worker.
	reports, err := svc.LateOrders(context.Background(), time.Now().UTC(), usecase.LateSweepLimit)
	if err != nil || len(reports) != 1 || reports[0].OrgID != k.orgID || len(reports[0].Orders) != 1 {
		t.Fatalf("LateOrders = %+v, %v; want one report for this restaurant", reports, err)
	}
	if title := reports[0].Title(); title != "1 order is running late" {
		t.Errorf("title = %q", title)
	}
}

// docs:end test-late-sweep

// lateAfterSetting declares the one setting the sweep reads. A registry that
// was never loaded from the database answers with the declared default,
// which is what a worker built outside the app needs.
func lateAfterSetting(t *testing.T, _ *gorbitaltest.App) *settings.Setting[time.Duration] {
	t.Helper()
	reg := settings.NewRegistry()
	return settings.Duration(reg, "orders.late_after", 30*time.Minute)
}

// TestPlacingIsRateLimited: one customer can't flood a kitchen, however many
// devices they use. The limiter is named, so an operator can reset a
// customer's budget in /ops/auth/rate-limits.
func TestPlacingIsRateLimited(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, _ := app.SignUp(t, "diner@example.com")
	limited := false
	for range 25 {
		if place(t, diner, k, simple(k)).Status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("25 orders in a row were all accepted; the rate limit isn't doing anything")
	}
}

// TestCancelling: a customer may change their mind until the kitchen has the
// order ready, and only for their own order.
func TestCancelling(t *testing.T) {
	app := newApp(t)
	k := openRestaurant(t, app, "bruno@example.com", "Trattoria Bruno")
	diner, dinerID := app.SignUp(t, "diner@example.com")
	stranger, _ := app.SignUp(t, "stranger@example.com")

	res := place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var order apiOrder
	res.JSON(t, &order)
	path := "/v1/orders/" + order.ID

	stranger.Post(path+"/cancel", noReason).AssertProblem(t, http.StatusNotFound, "order_not_found")
	res = diner.Post(path+"/cancel", map[string]string{"reason": "ordered twice by mistake"})
	res.AssertStatus(t, http.StatusOK)
	var cancelled apiOrder
	res.JSON(t, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	diner.Post(path+"/cancel", noReason).AssertProblem(t, http.StatusConflict, "invalid_order_transition")

	// Once the kitchen has it ready, it is too late.
	res = place(t, diner, k, basket(k))
	res.AssertStatus(t, http.StatusCreated)
	var second apiOrder
	res.JSON(t, &second)
	authorisePayment(t, app, k, second, dinerID)
	staffPath := "/v1/orgs/" + k.orgID + "/orders/" + second.ID
	for _, step := range []string{"/accept", "/preparing", "/ready"} {
		k.staff.Post(staffPath+step, nil).AssertStatus(t, http.StatusOK)
	}
	diner.Post("/v1/orders/"+second.ID+"/cancel", noReason).
		AssertProblem(t, http.StatusConflict, "invalid_order_transition")
}
