package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// listOptions are the page sizes and sorts ListRestaurants accepts.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"created_at", "updated_at", "name", "address", "cuisine"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
}

// ListRestaurantsInput selects a page of an organisation's restaurants.
type ListRestaurantsInput struct {
	Page page.Params
	// Status keeps restaurants with this status; empty keeps all.
	Status domain.Status
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort string    `json:"s"`
	Time time.Time `json:"t,omitzero"`
	Text string    `json:"x,omitempty"`
	ID   string    `json:"i"`
}

// ListRestaurants returns a page of the organisation orgID's restaurants, sorted by
// one field (newest first by default).
func (s *Service) ListRestaurants(ctx context.Context, orgID string, in ListRestaurantsInput) (page.Result[domain.Restaurant], error) {
	var none page.Result[domain.Restaurant]
	if _, err := memberID(ctx, orgID); err != nil {
		return none, err
	}
	req, err := in.Page.Request(listOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 {
		return none, fmt.Errorf("%w: sort by one field", page.ErrInvalidSort)
	}
	if in.Status != "" && !in.Status.Valid() {
		return none, &domain.ValidationError{Errors: []domain.FieldError{{Field: "status", Message: "must be onboarding, open, paused or suspended"}}}
	}

	q := ListQuery{
		OrgID:  orgID,
		Status: in.Status,
		Sort:   req.Sort[0],
		// One more than the limit shows whether there is a next page.
		Limit: req.Limit + 1,
	}
	sort := sortName(q.Sort)
	if req.Cursor != "" {
		var c cursor
		if err := page.DecodeCursor(req.Cursor, &c); err != nil {
			return none, err
		}
		if c.Sort != sort || c.ID == "" {
			return none, fmt.Errorf("%w: it belongs to a different sort", page.ErrInvalidCursor)
		}
		q.After = &Position{Time: c.Time, Text: c.Text, ID: c.ID}
	}

	items, err := s.store.SelectRestaurants(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := page.Result[domain.Restaurant]{Items: items}
	if len(items) > req.Limit {
		res.Items = items[:req.Limit]
		last := res.Items[len(res.Items)-1]
		c := cursor{Sort: sort, ID: last.ID}
		switch q.Sort.Field {
		case "created_at":
			c.Time = last.CreatedAt
		case "updated_at":
			c.Time = last.UpdatedAt
		case "name":
			c.Text = last.Name
		case "address":
			c.Text = last.Address
		case "cuisine":
			c.Text = last.Cuisine
		}
		if res.NextCursor, err = page.EncodeCursor(c); err != nil {
			return none, err
		}
	}
	return res, nil
}

func sortName(f page.SortField) string {
	if f.Desc {
		return "-" + f.Field
	}
	return f.Field
}
