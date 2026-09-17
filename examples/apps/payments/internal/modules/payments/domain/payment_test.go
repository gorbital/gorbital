package domain_test

import (
	"errors"
	"testing"
	"time"

	"example.com/payments/internal/modules/payments/domain"
)

func TestNewPayment(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	valid := domain.Event{
		ID: "evt_1", Type: domain.EventSucceeded, PaymentID: "pi_3Qabc",
		AmountMinor: 4999, Currency: "gbp", OccurredAt: now.Add(-time.Minute),
	}

	tests := map[string]struct {
		change func(*domain.Event)
		want   error
	}{
		"valid":            {func(*domain.Event) {}, nil},
		"no payment ID":    {func(e *domain.Event) { e.PaymentID = "  " }, domain.ErrPaymentIDRequired},
		"zero amount":      {func(e *domain.Event) { e.AmountMinor = 0 }, domain.ErrInvalidAmount},
		"negative amount":  {func(e *domain.Event) { e.AmountMinor = -1 }, domain.ErrInvalidAmount},
		"two-letter code":  {func(e *domain.Event) { e.Currency = "GB" }, domain.ErrInvalidCurrency},
		"code with digits": {func(e *domain.Event) { e.Currency = "G8P" }, domain.ErrInvalidCurrency},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := valid
			tc.change(&e)
			p, err := domain.NewPayment("pay_1", domain.StatusSucceeded, e, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewPayment = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if p.Currency != "GBP" {
				t.Errorf("Currency = %q, want the code upper-cased", p.Currency)
			}
			if !p.RecordedAt.Equal(now) || !p.OccurredAt.Equal(valid.OccurredAt) {
				t.Errorf("times = %v, %v; want %v, %v", p.OccurredAt, p.RecordedAt, valid.OccurredAt, now)
			}
		})
	}

	// A provider that sends no time: the payment happened when we heard of
	// it, rather than in the year zero.
	e := valid
	e.OccurredAt = time.Time{}
	if p, err := domain.NewPayment("pay_1", domain.StatusSucceeded, e, now); err != nil || !p.OccurredAt.Equal(now) {
		t.Errorf("without occurred_at = %v, %v; want now", p.OccurredAt, err)
	}
}

func TestStatusFor(t *testing.T) {
	for eventType, want := range map[string]domain.Status{
		domain.EventSucceeded: domain.StatusSucceeded,
		domain.EventRefunded:  domain.StatusRefunded,
		"payout.paid":         "",
		"":                    "",
	} {
		got, ok := domain.StatusFor(eventType)
		if got != want || ok != (want != "") {
			t.Errorf("StatusFor(%q) = %q, %v; want %q", eventType, got, ok, want)
		}
	}
}
