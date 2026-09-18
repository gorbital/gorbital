package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start list-options

// listOptions are the page sizes and sorts the order lists accept. A sort
// the list doesn't name is refused with 400 invalid_sort before any SQL is
// built, and the repository keeps one fixed query per sort, so nothing a
// request sends ever becomes SQL text.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"placed_at", "total_minor"},
		DefaultSort: []page.SortField{{Field: "placed_at", Desc: true}},
	}
}

// docs:end list-options

// ListOrdersInput narrows a page of orders.
type ListOrdersInput struct {
	Page page.Params
	// Status keeps orders with this status; empty keeps all.
	Status domain.Status
	// From and Before bound when the orders were placed; zero means open.
	From   time.Time
	Before time.Time
	// CourierID keeps orders carried by one courier; empty keeps all. Only
	// a restaurant's own list offers it.
	CourierID string
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort  string    `json:"s"`
	Time  time.Time `json:"t,omitzero"`
	Total int64     `json:"n,omitempty"`
	ID    string    `json:"i"`
}

// docs:start three-lists

// ListOrgOrders returns a page of one restaurant's orders, newest first
// unless the request says otherwise.
func (s *Service) ListOrgOrders(ctx context.Context, orgID string, in ListOrdersInput) (page.Result[domain.Order], error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return page.Result[domain.Order]{}, err
	}
	return s.list(ctx, ListQuery{OrgID: orgID, CourierID: in.CourierID}, in)
}

// ListMyOrders returns a page of the orders the caller placed, across every
// restaurant they have ordered from.
func (s *Service) ListMyOrders(ctx context.Context, in ListOrdersInput) (page.Result[domain.Order], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return page.Result[domain.Order]{}, err
	}
	return s.list(ctx, ListQuery{CustomerID: caller}, in)
}

// ListMyDeliveries returns a page of the orders assigned to the caller's
// courier profile.
func (s *Service) ListMyDeliveries(ctx context.Context, in ListOrdersInput) (page.Result[domain.Order], error) {
	caller, err := callerID(ctx)
	if err != nil {
		return page.Result[domain.Order]{}, err
	}
	courier, err := s.store.CourierOfUser(ctx, caller)
	if err != nil {
		return page.Result[domain.Order]{}, storeError("courier", err)
	}
	return s.list(ctx, ListQuery{CourierID: courier}, in)
}

// docs:end three-lists

// docs:start list-orders

// list is the one paging path all three lists take. Its first argument is
// the filter that decides which rows exist for this caller — one
// restaurant's, one customer's, or one courier's — and it is applied to
// every page.
//
// It is not carried in the cursor, and that is the point worth reading
// twice. page.EncodeCursor produces an opaque string, but opaque is not
// signed: a client can decode one, and a cursor from somewhere else is just
// a position. If the organisation or the customer lived inside it, a pasted
// cursor would be an authorisation bypass. Keeping the filter in the query
// and the position in the cursor means the worst a stolen cursor can do is
// start the page in an odd place.
func (s *Service) list(ctx context.Context, q ListQuery, in ListOrdersInput) (page.Result[domain.Order], error) {
	var none page.Result[domain.Order]
	req, err := in.Page.Request(listOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 {
		return none, fmt.Errorf("%w: sort by one field", page.ErrInvalidSort)
	}
	if in.Status != "" && !in.Status.Valid() {
		return none, &domain.ValidationError{Errors: []domain.FieldError{{Field: "status", Message: "is not an order status"}}}
	}
	q.Status, q.PlacedFrom, q.PlacedBefore = in.Status, in.From, in.Before
	q.Sort, q.Limit = req.Sort[0], req.Limit+1 // one more than the limit shows whether there is a next page

	sort := sortName(q.Sort)
	if req.Cursor != "" {
		var c cursor
		if err := page.DecodeCursor(req.Cursor, &c); err != nil {
			return none, err
		}
		if c.Sort != sort || c.ID == "" {
			return none, fmt.Errorf("%w: it belongs to a different sort", page.ErrInvalidCursor)
		}
		q.After = &Position{Time: c.Time, Total: c.Total, ID: c.ID}
	}

	items, err := s.store.SelectOrders(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := page.Result[domain.Order]{Items: items}
	if len(items) > req.Limit {
		res.Items = items[:req.Limit]
		last := res.Items[len(res.Items)-1]
		c := cursor{Sort: sort, ID: last.ID}
		switch q.Sort.Field {
		case "placed_at":
			c.Time = last.PlacedAt
		case "total_minor":
			c.Total = last.TotalMinor
		}
		if res.NextCursor, err = page.EncodeCursor(c); err != nil {
			return none, err
		}
	}

	// docs:end list-orders
	ids := make([]string, len(res.Items))
	for i, o := range res.Items {
		ids[i] = o.ID
	}
	lines, err := s.store.SelectLines(ctx, ids)
	if err != nil {
		return none, storeError("lines", err)
	}
	for i := range res.Items {
		res.Items[i].Lines = lines[res.Items[i].ID]
	}
	return res, nil
}

func sortName(f page.SortField) string {
	if f.Desc {
		return "-" + f.Field
	}
	return f.Field
}
