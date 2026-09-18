package payments

import (
	"context"
	"crypto/rand"
	"encoding/base32"

	"example.com/plateful/internal/modules/payments/usecase"
)

// provider is the one adapter a real integration replaces. It stands in for
// a payment provider generically: asking for money returns a reference and
// moves nothing, and the answer arrives later as a signed webhook. That is
// the shape of every card provider worth using, and it is the only thing
// the rest of this module knows about them.
//
// Swapping it for a real one is a change to this file: call the provider's
// SDK in Pay and Refund, return its reference, and translate its event type
// names in domain.StatusFor. Nothing else in the module mentions a vendor,
// so nothing else has to change.
type provider struct{}

var _ usecase.Provider = provider{}

// newProvider returns the module's payment provider adapter.
func newProvider() usecase.Provider { return provider{} }

var refEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// Pay creates the payment at the provider and returns its reference. A real
// provider is asked over the network and may refuse; this one always
// agrees, because what the module has to get right is what happens between
// the reference coming back and the event arriving.
func (provider) Pay(_ context.Context, _ usecase.PayRequest) (string, error) {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "pref_" + refEncoding.EncodeToString(b), nil
}

// Refund asks the provider to give back what it took. A real provider
// records the reason against the payment; this one accepts it and does
// nothing, and the caller's transaction is what keeps the row honest.
func (provider) Refund(_ context.Context, _, _ string) error { return nil }
