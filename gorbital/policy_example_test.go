package gorbital_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
)

// An order, as a --scope custom module's domain package defines it.
type order struct {
	ID        string
	CreatedBy string
	CourierID string
	Delivered bool
}

// policy is what orb gen module --scope custom writes, before anyone has
// decided the rule. Every method refuses, and says why in a way the build
// can see.
type policy struct{}

func (policy) CanRead(ctx context.Context, o order) error {
	return gorbital.ErrNotImplemented // decide: which actors may read one order?
}

func ExampleErrNotImplemented() {
	// Nobody has written the rule yet, so nothing is served.
	err := policy{}.CanRead(context.Background(), order{ID: "ord_1"})
	fmt.Println(errors.Is(err, gorbital.ErrNotImplemented))

	// The module maps it to 501, so an unwritten rule refuses rather than
	// opens: "the app has not decided" is not "everyone may".
	mapping := httpx.Mapping{
		Err: gorbital.ErrNotImplemented, Status: http.StatusNotImplemented,
		Code: "not_implemented", Detail: "the orders access policy isn't written yet",
	}
	fmt.Println(mapping.Status, mapping.Code)
	// Output:
	// true
	// 501 not_implemented
}

// ExampleErrNotImplemented_written is the same policy once the rule
// exists: the courier assigned to an order may read it until it is
// delivered, and so may whoever placed it.
func ExampleErrNotImplemented_written() {
	canRead := func(actorID string, o order) error {
		switch {
		case actorID == o.CreatedBy:
			return nil
		case actorID == o.CourierID && !o.Delivered:
			return nil
		}
		return errors.New("orders: order not found") // never "forbidden": it doesn't exist for them
	}
	o := order{ID: "ord_1", CreatedBy: "usr_ada", CourierID: "usr_bo"}
	fmt.Println(canRead("usr_bo", o) == nil)
	o.Delivered = true
	fmt.Println(canRead("usr_bo", o) == nil)
	fmt.Println(canRead("usr_ada", o) == nil)
	// Output:
	// true
	// false
	// true
}
