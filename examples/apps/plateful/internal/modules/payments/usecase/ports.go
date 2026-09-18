package usecase

import (
	"context"

	"example.com/plateful/internal/modules/payments/domain"
)

// Order is the part of an order the payments module needs: who may pay for
// it, how much, and whether it is still waiting to be paid.
//
// It is deliberately not the orders module's own type. A module never
// imports another module's layers, so repository.SelectOrder reads the
// orders table with SQL and fills this in
// (internal/modules/payments/repository/select_order.go). The price of that
// is a contract in two places — the five columns in the SQL, and these five
// fields — which is what makes the orders table's column names public API.
type Order struct {
	OrgID      string
	CustomerID string
	// Status is the orders module's own status, as text. Only one of its
	// values means anything here: "placed", an order the restaurant has not
	// answered yet.
	Status string
	// TotalMinor is the order's total in integer minor units, never a
	// float. The payment is for exactly this, so the two can be reconciled
	// by subtraction rather than by rounding.
	TotalMinor int64
	Currency   string
}

// StatusPlaced is the only order status a customer can pay for: the
// restaurant has been given the order and has not accepted or rejected it
// yet. The literal is the orders module's, read from its table.
const StatusPlaced = "placed"

// Payable reports whether this order is one a customer can still pay for.
func (o Order) Payable() bool { return o.Status == StatusPlaced }

// Store reads and writes payments; repository.Store implements it with SQL.
type Store interface {
	// SelectOrder returns the order's payment-relevant columns, or
	// domain.ErrOrderNotFound. It reads the orders module's table directly,
	// with SQL and no Go import.
	SelectOrder(ctx context.Context, orderID string) (Order, error)
	// InsertPayment stores a new payment, or returns
	// domain.ErrPaymentNotPayable when the order already has one.
	InsertPayment(ctx context.Context, p domain.Payment) (domain.Payment, error)
	// SelectPaymentByOrder returns the order's payment, or
	// domain.ErrPaymentNotFound. lock locks the row until the transaction
	// ends.
	SelectPaymentByOrder(ctx context.Context, orderID string, lock bool) (domain.Payment, error)
	// SelectPayment returns one payment by ID, or
	// domain.ErrPaymentNotFound. lock locks the row until the transaction
	// ends.
	SelectPayment(ctx context.Context, id string, lock bool) (domain.Payment, error)
	// UpdatePayment saves p when the stored version is still p.Version and
	// increments the version. It returns domain.ErrPaymentNotFound when no
	// row has that version.
	UpdatePayment(ctx context.Context, p domain.Payment) (domain.Payment, error)
	// InsertEvent records that the provider's event has been seen, and
	// reports whether this call was the one that recorded it. A redelivery
	// of an event already recorded returns false and changes nothing.
	InsertEvent(ctx context.Context, e domain.Event) (bool, error)
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}

// docs:start payment-provider-port

// Provider is the payment provider, modelled generically: Pay asks it for
// the money and gets back a reference, and the provider says later, by
// webhook, whether it was authorised, captured or failed. No provider's API
// is described anywhere in this module.
//
// A real integration replaces the one adapter behind this interface
// (internal/modules/payments/provider.go) and nothing else: the status
// machine, the routes, the idempotency and the cross-module rule all work
// against these two methods and the webhook body, not against a vendor.
type Provider interface {
	// Pay asks the provider to take amountMinor of currency for the order,
	// and returns its own reference for the payment it created. The money
	// has not moved when it returns: the provider confirms by webhook.
	Pay(ctx context.Context, req PayRequest) (string, error)
	// Refund asks the provider to give back what it took for providerRef.
	// The reason is for the provider's own records.
	Refund(ctx context.Context, providerRef, reason string) error
}

// PayRequest is what the provider is told about a payment being created.
type PayRequest struct {
	OrderID     string
	CustomerID  string
	AmountMinor int64
	Currency    string
}

// docs:end payment-provider-port
