package delivery

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

// OrderPage is a page of orders.
type OrderPage struct {
	Items      []OrderResponse `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type orderPageOutput struct {
	Body OrderPage
}

// docs:start list-filters

// The narrowing parameters both order lists accept. They are a filter over
// rows the caller may already see: which rows those are is decided by the
// route, not by anything here, so a customer can't turn their own list into
// somebody else's by sending a cleverer query.
//
// They are written out in each input struct rather than shared through an
// embedded one. Huma reads the query tags of a struct embedded directly in
// the input, and stops there: a second level of embedding is silently
// ignored, and a `limit` that quietly does nothing is a bad afternoon.
//
// Huma also refuses a pointer in a query parameter, so a time that may be
// absent is a string and "" means absent, parsed by parseTime into the
// module's own validation problem rather than a 500.

// parseTime reads an RFC 3339 time from a query parameter; "" is no time at
// all, which every caller of it treats as an open bound.
func parseTime(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, &domain.ValidationError{Errors: []domain.FieldError{
			{Field: field, Message: "must be a time such as 2026-09-18T19:00:00Z"},
		}}
	}
	return at.UTC(), nil
}

// docs:end list-filters

func filters(p page.Params, status, from, before string) (usecase.ListOrdersInput, error) {
	in := usecase.ListOrdersInput{Page: p, Status: domain.Status(status)}
	var err error
	if in.From, err = parseTime("from", from); err != nil {
		return in, err
	}
	in.Before, err = parseTime("before", before)
	return in, err
}

type listOrgOrdersInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status    string `query:"status" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled" doc:"Only orders with this status"`
	From      string `query:"from" format:"date-time" doc:"Only orders placed at or after this time (RFC 3339)"`
	Before    string `query:"before" format:"date-time" doc:"Only orders placed before this time (RFC 3339)"`
	CourierID string `query:"courier_id" maxLength:"64" doc:"Only orders carried by this courier"`
}

func (h handlers) listOrders(ctx context.Context, in *listOrgOrdersInput) (*orderPageOutput, error) {
	f, err := filters(in.Params, in.Status, in.From, in.Before)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	f.CourierID = in.CourierID
	filters := f
	res, err := h.svc.ListOrgOrders(ctx, in.OrgID, filters)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	return toPage(res), nil
}

type listMyOrdersInput struct {
	page.Params
	Status string `query:"status" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled" doc:"Only orders with this status"`
	From   string `query:"from" format:"date-time" doc:"Only orders placed at or after this time (RFC 3339)"`
	Before string `query:"before" format:"date-time" doc:"Only orders placed before this time (RFC 3339)"`
}

func (h handlers) listMyOrders(ctx context.Context, in *listMyOrdersInput) (*orderPageOutput, error) {
	f, err := filters(in.Params, in.Status, in.From, in.Before)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	res, err := h.svc.ListMyOrders(ctx, f)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	return toPage(res), nil
}

func (h handlers) listMyDeliveries(ctx context.Context, in *listMyOrdersInput) (*orderPageOutput, error) {
	f, err := filters(in.Params, in.Status, in.From, in.Before)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	res, err := h.svc.ListMyDeliveries(ctx, f)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	return toPage(res), nil
}

func toPage(res page.Result[domain.Order]) *orderPageOutput {
	out := &orderPageOutput{Body: OrderPage{Items: make([]OrderResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, o := range res.Items {
		out.Body.Items[i] = toResponse(o)
	}
	return out
}
