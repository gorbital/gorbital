package usecase

import (
	"context"
	"fmt"
	"slices"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
)

// LateSweepLimit is how many late orders one sweep reads. A run that fills
// it reports what it found; the next run, minutes later, sees the rest.
const LateSweepLimit = 500

// MaxLateLines is how many orders one notification names, so a restaurant
// having a bad night gets a message somebody will read.
const MaxLateLines = 10

// A LateReport is one restaurant's late orders, as the sweep found them.
type LateReport struct {
	OrgID  string
	Orders []domain.Order
}

// Title is the notification's heading.
func (r LateReport) Title() string {
	if len(r.Orders) == 1 {
		return "1 order is running late"
	}
	return fmt.Sprintf("%d orders are running late", len(r.Orders))
}

// Lines are the notification's lines, the order kept longest first, at most
// MaxLateLines of them.
func (r LateReport) Lines(now time.Time) []string {
	lines := make([]string, 0, min(len(r.Orders), MaxLateLines)+1)
	for _, o := range r.Orders[:min(len(r.Orders), MaxLateLines)] {
		lines = append(lines, fmt.Sprintf("%s — %s for %s",
			o.ID, o.Status, now.Sub(o.AcceptedAt).Round(time.Minute)))
	}
	if extra := len(r.Orders) - MaxLateLines; extra > 0 {
		lines = append(lines, fmt.Sprintf("…and %d more", extra))
	}
	return lines
}

// docs:start late-orders

// LateOrders groups the platform's late orders at now by restaurant: the
// ones a kitchen accepted more than orders.late_after ago and hasn't
// delivered, the oldest first within each.
//
// No route calls it. It is the orders_late_sweep job's only reason to exist,
// and it lives here, beside the operations the API serves, because a use
// case is the unit that holds a rule — whether an HTTP request, a worker or
// a command is the one asking. Not every function has to be an endpoint, and
// the ones that aren't shouldn't be hidden in the worker either, where
// nothing else could reach them and no test could call them directly.
//
// It reads across organisations, which no request ever does, so it takes the
// time to compare against rather than reading a clock of its own: the job
// passes the moment it started, and a test passes a time it chose.
func (s *Service) LateOrders(ctx context.Context, now time.Time, limit int) ([]LateReport, error) {
	cutoff := now.Add(-s.lateAfter.Get(ctx))
	orders, err := s.store.SelectLateOrders(ctx, cutoff, limit)
	if err != nil {
		return nil, storeError("late", err)
	}
	byOrg := map[string]int{}
	var reports []LateReport
	for _, o := range orders {
		i, ok := byOrg[o.OrgID]
		if !ok {
			i = len(reports)
			byOrg[o.OrgID] = i
			reports = append(reports, LateReport{OrgID: o.OrgID})
		}
		reports[i].Orders = append(reports[i].Orders, o)
	}
	slices.SortFunc(reports, func(a, b LateReport) int {
		return a.Orders[0].AcceptedAt.Compare(b.Orders[0].AcceptedAt)
	})
	return reports, nil
}

// docs:end late-orders
