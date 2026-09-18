package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/plateful/internal/modules/payments/domain"
)

var now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func newPending(t *testing.T) domain.Payment {
	t.Helper()
	p, err := domain.NewPayment("pay_1", "org_1", "ord_1", "usr_1", 2350, "gbp", "pref_1", now)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if p.Status != domain.StatusPending || p.Currency != "GBP" {
		t.Fatalf("new payment = %+v, want a pending GBP payment", p)
	}
	return p
}

// TestEveryTransition walks the whole machine: the arrows it has, the ones
// it hasn't, and the two statuses nothing leaves.
func TestEveryTransition(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to domain.Status
		want     error
	}{
		"pending to authorised":  {domain.StatusPending, domain.StatusAuthorised, nil},
		"pending to failed":      {domain.StatusPending, domain.StatusFailed, nil},
		"authorised to captured": {domain.StatusAuthorised, domain.StatusCaptured, nil},
		"authorised to refunded": {domain.StatusAuthorised, domain.StatusRefunded, nil},
		"captured to refunded":   {domain.StatusCaptured, domain.StatusRefunded, nil},
		"pending to captured":    {domain.StatusPending, domain.StatusCaptured, domain.ErrPaymentNotPayable},
		"pending to refunded":    {domain.StatusPending, domain.StatusRefunded, domain.ErrPaymentNotPayable},
		"captured to authorised": {domain.StatusCaptured, domain.StatusAuthorised, domain.ErrPaymentNotPayable},
		"refunded to captured":   {domain.StatusRefunded, domain.StatusCaptured, domain.ErrPaymentAlreadyFinal},
		"failed to authorised":   {domain.StatusFailed, domain.StatusAuthorised, domain.ErrPaymentAlreadyFinal},
	} {
		t.Run(name, func(t *testing.T) {
			p := newPending(t)
			p.Status = tc.from
			moved, err := p.MoveTo(tc.to, "the card was declined", now.Add(time.Minute))
			if !errors.Is(err, tc.want) {
				t.Fatalf("MoveTo(%s) from %s = %v, want %v", tc.to, tc.from, err, tc.want)
			}
			if tc.want != nil {
				return
			}
			if moved.Status != tc.to {
				t.Errorf("status = %s, want %s", moved.Status, tc.to)
			}
			// The reason is kept for a failure and for nothing else: a
			// captured payment has no explaining to do.
			if wantReason := tc.to == domain.StatusFailed; (moved.FailureReason != "") != wantReason {
				t.Errorf("failure_reason = %q, want it kept only for a failure", moved.FailureReason)
			}
		})
	}
}

// TestRefundIsRefusedTwice: the arrow to refunded exists once. A payment
// that never took money and one already given back are the same answer.
func TestRefundIsRefusedTwice(t *testing.T) {
	p := newPending(t)
	if _, err := p.Refund(now); !errors.Is(err, domain.ErrPaymentNotRefundable) {
		t.Errorf("refunding a pending payment = %v, want ErrPaymentNotRefundable", err)
	}

	authorised, err := p.MoveTo(domain.StatusAuthorised, "", now)
	if err != nil {
		t.Fatal(err)
	}
	refunded, err := authorised.Refund(now)
	if err != nil || refunded.Status != domain.StatusRefunded {
		t.Fatalf("Refund = %+v, %v; want a refunded payment", refunded, err)
	}
	if _, err := refunded.Refund(now); !errors.Is(err, domain.ErrPaymentNotRefundable) {
		t.Errorf("refunding twice = %v, want ErrPaymentNotRefundable", err)
	}
	if !refunded.Status.Final() {
		t.Error("refunded isn't final; nothing should leave it")
	}
}

// TestStatusForOnlyKnowsWhatItActsOn: an event type the app doesn't act on
// is not an error, it is a word this app hasn't learned.
func TestStatusForOnlyKnowsWhatItActsOn(t *testing.T) {
	for kind, want := range map[string]domain.Status{
		domain.EventAuthorised: domain.StatusAuthorised,
		domain.EventCaptured:   domain.StatusCaptured,
		domain.EventFailed:     domain.StatusFailed,
	} {
		if got, ok := domain.StatusFor(kind); !ok || got != want {
			t.Errorf("StatusFor(%q) = %q, %v; want %q, true", kind, got, ok, want)
		}
	}
	if _, ok := domain.StatusFor("payment.disputed"); ok {
		t.Error("StatusFor(payment.disputed) reported a status; the app doesn't act on it")
	}
}

// TestEventCleanNeedsAnIDAndAPayment: the provider signs what it sends, and
// promises nothing about the lengths this app's columns accept.
func TestEventCleanNeedsAnIDAndAPayment(t *testing.T) {
	if _, err := (domain.Event{Kind: domain.EventAuthorised, PaymentID: "pay_1"}).Clean(); !errors.Is(err, domain.ErrInvalidPayment) {
		t.Errorf("an event with no ID = %v, want ErrInvalidPayment", err)
	}
	if _, err := (domain.Event{ID: "evt_1", Kind: domain.EventAuthorised}).Clean(); !errors.Is(err, domain.ErrInvalidPayment) {
		t.Errorf("an event naming no payment = %v, want ErrInvalidPayment", err)
	}
	long := strings.Repeat("x", domain.MaxReasonLength+50)
	cleaned, err := domain.Event{ID: " evt_1 ", Kind: domain.EventFailed, PaymentID: "pay_1", Reason: long}.Clean()
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.ID != "evt_1" || len(cleaned.Reason) != domain.MaxReasonLength {
		t.Errorf("cleaned = %+v, want the ID trimmed and the reason bounded", cleaned)
	}
}
