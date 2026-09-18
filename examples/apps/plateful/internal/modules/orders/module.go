// Package orders is Plateful's orders module: a customer orders from one
// restaurant, the kitchen works through a state machine, and a courier
// carries it. It is in four layers (domain, usecase, repository, delivery)
// with one file per operation in each.
//
// It is the module where the platform's three kinds of caller meet one
// table, which is the hardest thing about multi-tenancy and the reason this
// example exists. A restaurant's staff reach their own orders through
// guard.OrgMember. A customer and a courier belong to no organisation at
// all, so their routes carry a platform permission that says only "somebody
// is signed in", and which rows they may touch is decided in the use cases.
// See usecase/get_order.go for the three shapes side by side.
package orders

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules/notifications"
	"example.com/plateful/internal/modules/orders/delivery"
	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/repository"
	"example.com/plateful/internal/modules/orders/usecase"
)

// docs:start module

// Module returns the orders module. main.go adds it with every other module
// through modules.All; its organisation routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes, permission
// names, setting keys, flag keys and job names are public API: add new ones,
// never change existing ones.
func Module() gorbital.Module {
	// Declared in Settings and Flags, before the stores exist, and used in
	// Routes and Jobs: no lookup by key anywhere.
	var (
		maxOpen    *settings.Setting[int]
		lateAfter  *settings.Setting[time.Duration]
		paused     *settings.Setting[bool]
		scheduled  *flags.Flag
		autoAssign *flags.Flag
	)
	config := func() usecase.Config {
		return usecase.Config{MaxOpen: maxOpen, LateAfter: lateAfter, Scheduled: scheduled, AutoAssign: autoAssign}
	}
	return gorbital.Module{
		Name: "orders",
		// docs:start errors
		// guard.OrgMember answers org_not_found for an organisation the
		// caller isn't a member of, and forbidden for a role without the
		// permission, so neither is mapped here. ordering_paused isn't
		// either: the module's middleware writes that problem itself, before
		// any of this runs.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidOrder, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the order is not valid"},
			{Err: domain.ErrOrderNotFound, Status: http.StatusNotFound, Code: "order_not_found", Detail: "no order you can see has this ID"},
			{Err: domain.ErrInvalidTransition, Status: http.StatusConflict, Code: "invalid_order_transition", Detail: "the order can't move to that status from the one it is in"},
			{Err: domain.ErrRestaurantNotFound, Status: http.StatusNotFound, Code: "restaurant_not_found", Detail: "no restaurant you can see has this ID"},
			{Err: domain.ErrRestaurantNotAccepting, Status: http.StatusConflict, Code: "restaurant_not_accepting", Detail: "the restaurant is not taking orders right now"},
			{Err: domain.ErrRestaurantBusy, Status: http.StatusConflict, Code: "restaurant_busy", Detail: "the kitchen has as many open orders as it can hold; try again shortly"},
			{Err: domain.ErrItemUnavailable, Status: http.StatusUnprocessableEntity, Code: "dish_unavailable", Detail: "a dish on the order is not on the menu today"},
			{Err: domain.ErrItemOutOfStock, Status: http.StatusConflict, Code: "dish_out_of_stock", Detail: "a dish on the order has run out"},
			{Err: domain.ErrPaymentNotAuthorised, Status: http.StatusConflict, Code: "payment_not_authorised", Detail: "the order's payment is not authorised yet"},
			{Err: domain.ErrCourierUnavailable, Status: http.StatusConflict, Code: "courier_unavailable", Detail: "the courier is not available or is already carrying an order"},
			{Err: domain.ErrNotACourier, Status: http.StatusNotFound, Code: "not_a_courier", Detail: "this account has no courier profile"},
			{Err: domain.ErrSchedulingUnavailable, Status: http.StatusUnprocessableEntity, Code: "scheduling_unavailable", Detail: "ordering for later is not available yet"},
		},
		// docs:end errors
		// docs:start permissions
		// Two catalogues, and a permission belongs to exactly one. The first
		// two are organisation permissions, held by a restaurant's staff
		// through their role. The last three are platform permissions held
		// by "user", the role every signed-in account has, because a
		// customer and a courier are members of nothing. They say the route
		// is for a signed-in person; the use cases say whose orders they may
		// touch.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your restaurant's orders", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermManage, Description: "Accept, reject and move your restaurant's orders along", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermPlace, Description: "Place and cancel your own orders", Roles: []string{"user"}},
			{Name: usecase.PermView, Description: "See an order you placed or are carrying", Roles: []string{"user"}},
			{Name: usecase.PermDeliver, Description: "Collect and deliver an order assigned to you", Roles: []string{"user"}},
		},
		// docs:end permissions
		// docs:start settings-and-flags
		Settings: func(r *settings.Registry) {
			maxOpen = settings.Int(r, "orders.max_open_per_restaurant", 50,
				settings.Describe("How many orders one restaurant can have in hand before it stops taking more. A busier order answers 409 restaurant_busy."),
				settings.Group("orders"), settings.Range(1, 5_000), settings.ReasonRequired())
			lateAfter = settings.Duration(r, "orders.late_after", 30*time.Minute,
				settings.Describe("How long a restaurant may hold an accepted order before the orders_late_sweep job tells it the order is running late."),
				settings.Group("orders"), settings.Range(5*time.Minute, 6*time.Hour), settings.ReasonRequired())
			paused = settings.Bool(r, "orders.ordering_paused", false,
				settings.Describe("Stops every customer from placing, cancelling or advancing an order, with 503 ordering_paused. For a bad deploy or a data migration; reading is left alone."),
				settings.Group("orders"), settings.ReasonRequired())
		},
		Flags: func(r *flags.Registry) {
			// Client: the customer app reads it from GET /v1/flags and shows
			// or hides the "order for later" control. The server refuses a
			// scheduled order while it is off, so a client that ignores the
			// flag doesn't get a different answer from one that reads it.
			scheduled = flags.Bool(r, "orders.scheduled_ordering",
				flags.Describe("Lets customers order for a later time. The apps read it from GET /v1/flags to show the control; the API refuses scheduled_for while it is off."),
				flags.Group("orders"), flags.Client())
			// No Client: nobody outside the platform needs to know, and
			// there is nothing for a client to draw. It decides whether a
			// courier is picked automatically when an order becomes ready.
			autoAssign = flags.Bool(r, "orders.courier_auto_assign",
				flags.Describe("Gives a ready order to a free courier automatically. Roll it out by organisation: the bucket subject is the restaurant's organisation, so a percentage moves whole restaurants at a time."),
				flags.Group("orders"))
		},
		// docs:end settings-and-flags
		// docs:start jobs
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			// Deps.Jobs is nil here: New calls this before it builds the job
			// client, because it builds the client from these definitions.
			// So the worker keeps what it needs from d — the pool, the
			// logger, a service — and the notifier it is given takes the
			// client from the context when the job actually runs. The
			// service has no transaction manager for the same reason; the
			// sweep only reads.
			svc := usecase.NewService(repository.NewStore(d.DB), nil, d.Audit, d.Logger, config())
			jobs.Define(defs, jobs.Definition[usecase.LateSweepArgs]{
				Name:        usecase.LateSweepJob,
				Description: "Tells a restaurant when it has been holding an accepted order for longer than orders.late_after, through its notification endpoints.",
				Worker:      usecase.NewLateSweepWorker(svc, notifications.FromWorker, d.Logger),
				NewArgs:     func() usecase.LateSweepArgs { return usecase.LateSweepArgs{} },
				Enabled:     true,
				Schedule:    "@every 5m",
				Timeout:     2 * time.Minute,
				MaxAttempts: 1, // the next sweep, five minutes later, looks again
				Queue:       "default",
				Priority:    3,
			})
		},
		// docs:end jobs
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), repository.NewTxManager(d.DB, d.Jobs), d.Audit, d.Logger, config())
			delivery.Register(r, svc, delivery.PauseOrdering(paused))
		},
	}
}

// docs:end module
