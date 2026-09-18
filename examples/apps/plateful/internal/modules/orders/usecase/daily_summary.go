package usecase

import (
	"context"
	"time"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start daily-summary

// DailySummary is one restaurant's day: how many orders it took under each
// status, what the delivered ones came to, how long the kitchen took on
// average, and which hour was busiest.
//
// Every one of those numbers is the database's answer to a single query
// (repository/select_daily_summary.go). Reading the day's orders into Go and
// counting them would be the same result and a much worse program: it moves
// every row across the wire to compute six numbers, it grows with the
// restaurant's success, and it makes the arithmetic something to maintain.
// A read model is still a use case — it checks the caller acts in the
// organisation and it names what the numbers mean — but the counting belongs
// in the repository, in SQL, where there is no ORM in the way.
//
// from is inclusive and to exclusive, so a day is [midnight, next midnight)
// and two consecutive days never count the same order twice.
func (s *Service) DailySummary(ctx context.Context, orgID string, from, to time.Time) (DailySummary, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return DailySummary{}, err
	}
	if !to.After(from) {
		return DailySummary{}, invalidRange()
	}
	if to.Sub(from) > 31*24*time.Hour {
		return DailySummary{}, invalidRange()
	}
	summary, err := s.store.SelectDailySummary(ctx, orgID, from.UTC(), to.UTC())
	if err != nil {
		return DailySummary{}, storeError("summary", err)
	}
	summary.From, summary.To = from.UTC(), to.UTC()
	return summary, nil
}

// docs:end daily-summary

// invalidRange is the refusal for a period that isn't a period.
func invalidRange() error {
	return &domain.ValidationError{Errors: []domain.FieldError{
		{Field: "to", Message: "must be after from, and at most 31 days later"},
	}}
}
