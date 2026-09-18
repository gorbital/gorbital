package domain_test

import (
	"errors"
	"testing"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
)

var now = time.Date(2026, 9, 18, 19, 0, 0, 0, time.UTC)

// basket is a valid order the tests start from: two pizzas at £12.50 and a
// side at £4.
func basket() domain.Basket {
	return domain.Basket{
		Address: "3 Kirkgate, Leeds",
		Lines: []domain.Line{
			{ItemID: "mnu_1", Name: "Margherita", PriceMinor: 1250, Quantity: 2},
			{ItemID: "mnu_2", Name: "Olives", PriceMinor: 400, Quantity: 1},
		},
	}
}

// docs:start test-total-is-exact

// TestTotalIsExactInMinorUnits: the total is integer arithmetic, so it is
// the sum of the lines to the penny. The same sum in floating point
// (12.50*2 + 4.00) is where a penny goes missing.
func TestTotalIsExactInMinorUnits(t *testing.T) {
	order, err := domain.NewOrder("ord_1", "org_1", "rst_1", "usr_1", "GBP", basket(), now)
	if err != nil {
		t.Fatal(err)
	}
	if order.TotalMinor != 2900 {
		t.Errorf("total = %d, want 2900 pence", order.TotalMinor)
	}
}

// docs:end test-total-is-exact

func TestNewOrderRules(t *testing.T) {
	for name, change := range map[string]func(*domain.Basket){
		"no address":   func(b *domain.Basket) { b.Address = "  " },
		"empty basket": func(b *domain.Basket) { b.Lines = nil },
		"no quantity":  func(b *domain.Basket) { b.Lines[0].Quantity = 0 },
		"too many":     func(b *domain.Basket) { b.Lines[0].Quantity = 100 },
	} {
		t.Run(name, func(t *testing.T) {
			b := basket()
			change(&b)
			if _, err := domain.NewOrder("ord_1", "org_1", "rst_1", "usr_1", "GBP", b, now); !errors.Is(err, domain.ErrInvalidOrder) {
				t.Errorf("NewOrder = %v, want an invalid order", err)
			}
		})
	}
}

// docs:start test-state-machine

// TestStateMachine walks the happy path and then tries every move the
// machine forbids. Each refusal is the same error, which module.go maps to
// 409 invalid_order_transition, so a second tap on "accept" never
// double-accepts and a courier can't deliver something the kitchen hasn't
// finished.
func TestStateMachine(t *testing.T) {
	order, err := domain.NewOrder("ord_1", "org_1", "rst_1", "usr_1", "GBP", basket(), now)
	if err != nil {
		t.Fatal(err)
	}

	for i, step := range []domain.Status{
		domain.StatusAccepted, domain.StatusPreparing, domain.StatusReady,
		domain.StatusCollected, domain.StatusDelivered,
	} {
		at := now.Add(time.Duration(i+1) * time.Minute)
		if order, err = order.MoveTo(step, "", at); err != nil {
			t.Fatalf("moving to %s: %v", step, err)
		}
		if order.Status != step {
			t.Fatalf("status = %s, want %s", order.Status, step)
		}
	}
	if order.AcceptedAt.IsZero() || order.ReadyAt.IsZero() || order.DeliveredAt.IsZero() || order.ClosedAt.IsZero() {
		t.Errorf("a delivered order = %+v, want every step stamped and the order closed", order)
	}

	// Delivered is terminal: nothing moves it, not even backwards.
	for _, to := range []domain.Status{domain.StatusDelivered, domain.StatusCancelled, domain.StatusReady} {
		if _, err := order.MoveTo(to, "", now); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("a delivered order moving to %s = %v, want ErrInvalidTransition", to, err)
		}
	}

	placed, _ := domain.NewOrder("ord_2", "org_1", "rst_1", "usr_1", "GBP", basket(), now)
	for _, to := range []domain.Status{domain.StatusReady, domain.StatusCollected, domain.StatusDelivered, domain.StatusPreparing} {
		if _, err := placed.MoveTo(to, "", now); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("a placed order jumping to %s = %v, want ErrInvalidTransition", to, err)
		}
	}
	// A customer may cancel a placed order, and a restaurant may reject it.
	for _, to := range []domain.Status{domain.StatusCancelled, domain.StatusRejected} {
		closed, err := placed.MoveTo(to, "out of dough", now)
		if err != nil || closed.ClosedAt.IsZero() || closed.ClosedReason != "out of dough" {
			t.Errorf("moving to %s = %+v, %v; want it closed with the reason", to, closed, err)
		}
	}
	// But not once it is ready and waiting for a courier.
	ready := placed
	for _, step := range []domain.Status{domain.StatusAccepted, domain.StatusPreparing, domain.StatusReady} {
		ready, _ = ready.MoveTo(step, "", now)
	}
	if _, err := ready.MoveTo(domain.StatusCancelled, "", now); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("cancelling a ready order = %v, want ErrInvalidTransition", err)
	}
}

// docs:end test-state-machine

func TestAssignCourier(t *testing.T) {
	order, _ := domain.NewOrder("ord_1", "org_1", "rst_1", "usr_1", "GBP", basket(), now)
	assigned, err := order.AssignCourier("cur_1", now)
	if err != nil || assigned.CourierID != "cur_1" {
		t.Fatalf("assigning = %+v, %v", assigned, err)
	}
	// Assigning the same courier again changes nothing, so a retried
	// request doesn't churn the row.
	again, err := assigned.AssignCourier("cur_1", now.Add(time.Hour))
	if err != nil || !again.UpdatedAt.Equal(assigned.UpdatedAt) {
		t.Errorf("assigning again = %+v, %v; want no change", again, err)
	}
	collected := assigned
	for _, step := range []domain.Status{domain.StatusAccepted, domain.StatusPreparing, domain.StatusReady, domain.StatusCollected} {
		collected, _ = collected.MoveTo(step, "", now)
	}
	if _, err := collected.AssignCourier("cur_2", now); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("reassigning a collected order = %v, want ErrInvalidTransition", err)
	}
}

func TestLate(t *testing.T) {
	order, _ := domain.NewOrder("ord_1", "org_1", "rst_1", "usr_1", "GBP", basket(), now)
	if order.Late(time.Minute, now.Add(time.Hour)) {
		t.Error("an order nobody has accepted is late; the clock starts when the kitchen takes it")
	}
	accepted, _ := order.MoveTo(domain.StatusAccepted, "", now)
	if accepted.Late(30*time.Minute, now.Add(20*time.Minute)) {
		t.Error("an order accepted 20 minutes ago is late at 30 minutes")
	}
	if !accepted.Late(30*time.Minute, now.Add(40*time.Minute)) {
		t.Error("an order accepted 40 minutes ago isn't late at 30 minutes")
	}
}
