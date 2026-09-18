package delivery

import (
	"context"
	"time"
)

// docs:start summary-response

// SummaryResponse is one restaurant's day, as the database counted it.
type SummaryResponse struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Orders is how many were placed in the period, and ByStatus how they
	// stand now — an order placed yesterday and delivered today counts as
	// delivered.
	Orders   int            `json:"orders"`
	ByStatus map[string]int `json:"by_status"`
	// RevenueMinor is what the delivered orders came to, in integer minor
	// units, with the currency beside it so the number is never ambiguous.
	RevenueMinor int64  `json:"revenue_minor" example:"48250"`
	Currency     string `json:"currency" example:"GBP"`
	// AveragePreparationSeconds is how long the kitchen took, on average,
	// between accepting an order and having it ready.
	AveragePreparationSeconds int `json:"average_preparation_seconds" example:"840"`
	// BusiestHour is the UTC hour that took the most orders, and how many.
	BusiestHour   int `json:"busiest_hour" example:"19"`
	BusiestOrders int `json:"busiest_orders" example:"14"`
}

// docs:end summary-response

type dailySummaryInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	// Huma refuses a pointer in a query parameter, so these are strings and
	// "" means "the default period".
	From string `query:"from" format:"date-time" doc:"The start of the period (RFC 3339); the last 24 hours by default"`
	To   string `query:"to" format:"date-time" doc:"The end of the period, exclusive (RFC 3339)"`
}

type summaryOutput struct {
	Body SummaryResponse
}

func (h handlers) dailySummary(ctx context.Context, in *dailySummaryInput) (*summaryOutput, error) {
	to, err := parseTime("to", in.To)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from, err := parseTime("from", in.From)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	summary, err := h.svc.DailySummary(ctx, in.OrgID, from, to)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	body := SummaryResponse{
		From: summary.From, To: summary.To, Orders: summary.Orders,
		ByStatus:                  make(map[string]int, len(summary.ByStatus)),
		RevenueMinor:              summary.RevenueMinor,
		Currency:                  summary.Currency,
		AveragePreparationSeconds: int(summary.AveragePreparation.Seconds()),
		BusiestHour:               summary.BusiestHour,
		BusiestOrders:             summary.BusiestOrders,
	}
	for status, n := range summary.ByStatus {
		body.ByStatus[string(status)] = n
	}
	return &summaryOutput{Body: body}, nil
}
