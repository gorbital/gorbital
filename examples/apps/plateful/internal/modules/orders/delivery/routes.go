// Package delivery is the orders module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/orders/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start order-routes

// Register adds the orders routes to r. They fall into two groups, and which
// guard a route carries is the clearest statement in the module of who it is
// for.
//
//   - The restaurant's own routes are under /v1/orgs/{orgId}/orders and
//     carry guard.OrgMember: the caller must be a member of that
//     organisation, with a role that holds the permission. Anyone else gets
//     404 org_not_found, as if the restaurant didn't exist.
//   - The customer's and the courier's routes carry guard.Permission with a
//     platform permission the "user" role holds — because neither is a
//     member of any organisation, and guard.OrgMember could never let them
//     in. That guard proves only that somebody is signed in; which orders
//     they may touch is decided in the use cases, by comparing the order
//     against the caller.
//
// Placing an order names the restaurant in the path rather than the body,
// and that is not a style choice: guard.Request can read path parameters,
// headers and the query, and never the body, so a restaurant named in the
// body could not be guarded at all. With it in the path, the module's own
// guard can refuse a suspended or closed restaurant before the handler runs.
func Register(r *gorbital.Router, svc *usecase.Service, pauseOrdering func(http.Handler) http.Handler) {
	h := handlers{svc: svc}

	// docs:start customer-routes
	// The customer-facing group. gorbital.Use puts the module's own
	// middleware in front of every route in it, before sign-in and the
	// guards run: while an operator has paused ordering in /ops/settings,
	// nothing here accepts a write, whoever is asking.
	customer := r.Group("/v1", gorbital.Tags("Ordering"), gorbital.Use(pauseOrdering))

	gorbital.Post(customer, "/restaurants/{restaurantId}/orders", h.placeOrder,
		gorbital.OperationID("orders-place"), gorbital.Summary("Place an order"),
		gorbital.Description("Prices the basket from the restaurant's menu and records the order. Send an `Idempotency-Key` header to make a retry safe."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermPlace),
		// The module's own guard: the restaurant has to be open. It reads
		// {restaurantId} from the path, which is all a guard can see.
		acceptingRestaurant(svc),
		// One customer can't flood a kitchen, however many devices they use.
		guard.RateLimit(20, time.Minute, guard.ByUser(), guard.Named(RateLimitPlace)))

	gorbital.Get(customer, "/orders", h.listMyOrders, gorbital.OperationID("orders-list-mine"),
		gorbital.Summary("List the orders you placed"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by `placed_at` or `total_minor`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest),
		guard.Permission(usecase.PermView))
	gorbital.Get(customer, "/orders/{id}", h.getOrder, gorbital.OperationID("orders-get-mine"),
		gorbital.Summary("Get an order you placed or are carrying"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermView))
	gorbital.Post(customer, "/orders/{id}/cancel", h.cancelOrder, gorbital.OperationID("orders-cancel"),
		gorbital.Summary("Cancel an order you placed"),
		gorbital.Description("Until the kitchen has it ready."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.Permission(usecase.PermPlace))

	gorbital.Get(customer, "/deliveries", h.listMyDeliveries, gorbital.OperationID("orders-list-deliveries"),
		gorbital.Summary("List the orders assigned to you as a courier"),
		gorbital.Errors(http.StatusBadRequest, http.StatusNotFound),
		guard.Permission(usecase.PermDeliver))
	gorbital.Post(customer, "/orders/{id}/collect", h.collectOrder, gorbital.OperationID("orders-collect"),
		gorbital.Summary("Collect an order you are carrying"),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.Permission(usecase.PermDeliver))
	gorbital.Post(customer, "/orders/{id}/deliver", h.deliverOrder, gorbital.OperationID("orders-deliver"),
		gorbital.Summary("Deliver an order you are carrying"),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.Permission(usecase.PermDeliver))
	// docs:end customer-routes

	// docs:start staff-routes
	staff := r.Group("/v1/orgs/{orgId}/orders", gorbital.Tags("Orders"))

	gorbital.Get(staff, "", h.listOrders, gorbital.OperationID("orders-list"),
		gorbital.Summary("List your restaurant's orders"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by `placed_at` or `total_minor`; narrow by `status`, `courier_id` and a date range. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(staff, "/summary", h.dailySummary, gorbital.OperationID("orders-summary"),
		gorbital.Summary("Count a day's orders"),
		gorbital.Description("Orders by status, the revenue of the delivered ones, the average time in the kitchen, and the busiest hour."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead),
		// One query over a month of orders is the most expensive thing this
		// module does, so it gets a deadline of its own rather than the
		// app's.
		gorbital.Timeout(10*time.Second))
	gorbital.Get(staff, "/{id}", h.getOrgOrder, gorbital.OperationID("orders-get"),
		gorbital.Summary("Get one of your restaurant's orders"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Post(staff, "/{id}/accept", h.acceptOrder, gorbital.OperationID("orders-accept"),
		gorbital.Summary("Accept an order"),
		gorbital.Description("Only once its payment is authorised."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.OrgMember(usecase.PermManage))
	gorbital.Post(staff, "/{id}/reject", h.rejectOrder, gorbital.OperationID("orders-reject"),
		gorbital.Summary("Reject an order"),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermManage))
	gorbital.Post(staff, "/{id}/preparing", h.startPreparing, gorbital.OperationID("orders-preparing"),
		gorbital.Summary("Start preparing an order"),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.OrgMember(usecase.PermManage))
	gorbital.Post(staff, "/{id}/ready", h.markReady, gorbital.OperationID("orders-ready"),
		gorbital.Summary("Mark an order ready"),
		gorbital.Description("A courier is assigned automatically when the `orders.courier_auto_assign` flag is on for your restaurant."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.OrgMember(usecase.PermManage))
	gorbital.Post(staff, "/{id}/courier", h.assignCourier, gorbital.OperationID("orders-assign-courier"),
		gorbital.Summary("Give an order to a courier"),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermManage))

	couriers := r.Group("/v1/orgs/{orgId}/available-couriers", gorbital.Tags("Orders"))
	gorbital.Get(couriers, "", h.availableCouriers, gorbital.OperationID("orders-available-couriers"),
		gorbital.Summary("List the couriers who are free"),
		gorbital.Description("Couriers belong to no restaurant: this proves you are a restaurant's staff, not that these couriers are yours."),
		guard.OrgMember(usecase.PermManage))
	// docs:end staff-routes
}

// docs:end order-routes
