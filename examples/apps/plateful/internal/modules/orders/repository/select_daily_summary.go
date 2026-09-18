package repository

import (
	"context"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

// docs:start daily-summary-sql

// selectDailySummarySQL counts one restaurant's day in one round trip: the
// orders under each status, what the delivered ones came to, how long the
// kitchen took between accepting an order and having it ready, and the hour
// that took the most.
//
// Three things are worth reading. The counts are FILTER clauses over a
// single scan, not four queries. The average preparation time is computed in
// the database from two timestamps, so no row has to travel to compute it.
// And the busiest hour is its own grouped sub-query joined on the left, so a
// day with no orders still answers — with zeroes, rather than with no row at
// all, which is why the counts are cross-joined and only the busiest hour is
// coalesced.
const selectDailySummarySQL = `
	WITH day AS (
	    SELECT status, total_minor, currency, placed_at, accepted_at, ready_at
	    FROM orders
	    WHERE org_id = $1 AND placed_at >= $2 AND placed_at < $3
	), counts AS (
	    SELECT count(*)                                              AS orders,
	           count(*) FILTER (WHERE status = 'placed')             AS placed,
	           count(*) FILTER (WHERE status = 'accepted')           AS accepted,
	           count(*) FILTER (WHERE status = 'preparing')          AS preparing,
	           count(*) FILTER (WHERE status = 'ready')              AS ready,
	           count(*) FILTER (WHERE status = 'collected')          AS collected,
	           count(*) FILTER (WHERE status = 'delivered')          AS delivered,
	           count(*) FILTER (WHERE status = 'rejected')           AS rejected,
	           count(*) FILTER (WHERE status = 'cancelled')          AS cancelled,
	           coalesce(sum(total_minor) FILTER (WHERE status = 'delivered'), 0) AS revenue_minor,
	           coalesce(max(currency), 'GBP')                        AS currency,
	           coalesce(avg(EXTRACT(EPOCH FROM (ready_at - accepted_at)))
	                    FILTER (WHERE ready_at IS NOT NULL AND accepted_at IS NOT NULL), 0) AS prep_seconds
	    FROM day
	), busiest AS (
	    SELECT EXTRACT(HOUR FROM placed_at AT TIME ZONE 'UTC')::int AS hour, count(*) AS orders
	    FROM day
	    GROUP BY 1
	    ORDER BY count(*) DESC, 1
	    LIMIT 1
	)
	SELECT c.orders, c.placed, c.accepted, c.preparing, c.ready, c.collected, c.delivered,
	       c.rejected, c.cancelled, c.revenue_minor, c.currency, c.prep_seconds,
	       coalesce(b.hour, 0), coalesce(b.orders, 0)
	FROM counts c LEFT JOIN busiest b ON true`

// SelectDailySummary counts one restaurant's orders between two times.
func (s *Store) SelectDailySummary(ctx context.Context, orgID string, from, to time.Time) (usecase.DailySummary, error) {
	var (
		orders, placed, accepted, preparing, ready, collected, delivered, rejected, cancelled int
		revenue                                                                               int64
		currency                                                                              string
		prepSeconds                                                                           float64
		busiestHour, busiestOrders                                                            int
	)
	err := s.db.QueryRow(ctx, selectDailySummarySQL, orgID, from, to).Scan(
		&orders, &placed, &accepted, &preparing, &ready, &collected, &delivered,
		&rejected, &cancelled, &revenue, &currency, &prepSeconds, &busiestHour, &busiestOrders)
	if err != nil {
		return usecase.DailySummary{}, err
	}
	return usecase.DailySummary{
		Orders: orders,
		ByStatus: map[domain.Status]int{
			domain.StatusPlaced:    placed,
			domain.StatusAccepted:  accepted,
			domain.StatusPreparing: preparing,
			domain.StatusReady:     ready,
			domain.StatusCollected: collected,
			domain.StatusDelivered: delivered,
			domain.StatusRejected:  rejected,
			domain.StatusCancelled: cancelled,
		},
		RevenueMinor:       revenue,
		Currency:           currency,
		AveragePreparation: time.Duration(prepSeconds * float64(time.Second)).Round(time.Second),
		BusiestHour:        busiestHour,
		BusiestOrders:      busiestOrders,
	}, nil
}

// docs:end daily-summary-sql
