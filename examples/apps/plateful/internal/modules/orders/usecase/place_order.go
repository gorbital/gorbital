package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
)

// PlaceOrderInput is what a customer sends: which dishes, how many, where to
// take it and when.
type PlaceOrderInput struct {
	Address      string
	Note         string
	ScheduledFor time.Time
	Items        []BasketItem
}

// A BasketItem is one line of the basket, before the menu has priced it.
type BasketItem struct {
	ItemID   string
	Quantity int
}

// docs:start place-order

// PlaceOrder records a customer's order at one restaurant.
//
// Everything it changes happens in one transaction: the order, its lines,
// the stock any limited dish gives up, and the job that tells the restaurant
// a new order has arrived. The job is written by jobs.Client.InsertTx
// through the same transaction (repository/tx.go), so it cannot escape a
// rollback — a worker announcing an order that was never committed is the
// ordinary bug here, and this is the shape that prevents it.
//
// The prices come from the menu inside the transaction, with the limited
// dishes' rows locked, and are then copied on to the order's lines. From
// that moment the order carries what the customer chose and was charged; a
// menu edited tomorrow cannot rewrite it.
func (s *Service) PlaceOrder(ctx context.Context, restaurantID string, in PlaceOrderInput) (domain.Order, error) {
	customer, err := callerID(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	if !in.ScheduledFor.IsZero() && !s.scheduled.Enabled(ctx) {
		// The same flag the customer app reads from GET /v1/flags to show
		// the control. A client that asks anyway is refused rather than
		// quietly ignored.
		return domain.Order{}, domain.ErrSchedulingUnavailable
	}
	if len(in.Items) == 0 {
		return domain.Order{}, &domain.ValidationError{Errors: []domain.FieldError{{Field: "items", Message: "an order needs at least one dish"}}}
	}
	// An order without an address goes where the customer usually wants
	// theirs taken, which they gave when they registered
	// (authhttp.RegisterFields, hooks.go). An account created by a first
	// Google sign-in never filled that form, so this can still be empty, and
	// then the domain refuses the order with a field error naming address.
	if strings.TrimSpace(in.Address) == "" {
		if in.Address, err = s.store.SelectCustomerAddress(ctx, customer); err != nil {
			return domain.Order{}, storeError("place", err)
		}
	}

	var placed domain.Order
	err = s.tx.InTx(ctx, func(tx Tx) error {
		orgID, accepting, err := tx.SelectRestaurant(ctx, restaurantID)
		switch {
		case err != nil:
			return err
		case !accepting:
			return domain.ErrRestaurantNotAccepting
		}

		// A restaurant with more open orders than its kitchen can hold stops
		// taking them. The limit is a runtime setting, so an operator raises
		// it on a busy Friday without a deploy.
		open, err := tx.CountOpenOrders(ctx, orgID)
		switch {
		case err != nil:
			return err
		case open >= s.maxOpen.Get(ctx):
			return domain.ErrRestaurantBusy
		}

		ids := make([]string, 0, len(in.Items))
		for _, item := range in.Items {
			ids = append(ids, item.ItemID)
		}
		slices.Sort(ids) // a stable lock order, so two baskets can't deadlock
		menu, err := tx.SelectMenuItems(ctx, orgID, slices.Compact(ids), true)
		if err != nil {
			return err
		}

		basket := domain.Basket{Address: in.Address, Note: in.Note, ScheduledFor: in.ScheduledFor}
		currency := "GBP"
		for _, item := range in.Items {
			dish, ok := menu[item.ItemID]
			switch {
			case !ok || !dish.Available:
				return fmt.Errorf("%w: %s", domain.ErrItemUnavailable, item.ItemID)
			case dish.Stock != nil && *dish.Stock < item.Quantity:
				return fmt.Errorf("%w: %s", domain.ErrItemOutOfStock, item.ItemID)
			}
			currency = dish.Currency
			basket.Lines = append(basket.Lines, domain.Line{
				ItemID: dish.ID, Name: dish.Name, PriceMinor: dish.PriceMin, Quantity: item.Quantity,
			})
		}

		order, err := domain.NewOrder(s.newID(), orgID, restaurantID, customer, currency, basket, s.clock())
		if err != nil {
			return err
		}
		if placed, err = tx.InsertOrder(ctx, order); err != nil {
			return err
		}
		for _, line := range order.Lines {
			if err := tx.TakeStock(ctx, orgID, line.ItemID, line.Quantity); err != nil {
				return err
			}
		}
		return tx.Notify(ctx, orgID, "A new order",
			[]string{fmt.Sprintf("%s — %d dishes, %s", placed.ID, len(placed.Lines), money(placed.TotalMinor, placed.Currency))})
	})
	if err != nil {
		return domain.Order{}, storeError("place", err)
	}

	// docs:end place-order
	s.audit(ctx, ActionPlaced, placed.ID, map[string]any{
		"restaurant_id": restaurantID, "lines": len(placed.Lines),
		"total_minor": placed.TotalMinor, "currency": placed.Currency,
	})
	return placed, nil
}

// money renders minor units for a message a person reads. It never does
// arithmetic on the value: the integer is the truth, and this is only how it
// is shown.
func money(minor int64, currency string) string {
	return fmt.Sprintf("%s %d.%02d", currency, minor/100, minor%100)
}
