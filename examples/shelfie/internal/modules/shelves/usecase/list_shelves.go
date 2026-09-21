package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// listOptions are the page sizes and sorts ListShelves accepts.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"created_at", "updated_at", "name"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
}

// ListShelvesInput selects a page of the signed-in user's shelves.
type ListShelvesInput struct {
	Page page.Params
	// Visibility keeps shelves with this visibility; empty keeps all.
	Visibility domain.Visibility
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort string    `json:"s"`
	Time time.Time `json:"t,omitzero"`
	Text string    `json:"x,omitempty"`
	ID   string    `json:"i"`
}

// ListShelves returns a page of the signed-in user's shelves, sorted by one
// field (newest first by default).
func (s *Service) ListShelves(ctx context.Context, in ListShelvesInput) (page.Result[domain.Shelf], error) {
	var none page.Result[domain.Shelf]
	owner, err := ownerID(ctx)
	if err != nil {
		return none, err
	}
	req, err := in.Page.Request(listOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 {
		return none, fmt.Errorf("%w: sort by one field", page.ErrInvalidSort)
	}
	if in.Visibility != "" && !in.Visibility.Valid() {
		return none, &domain.ValidationError{Errors: []domain.FieldError{{Field: "visibility", Message: "must be private or shared"}}}
	}

	q := ListQuery{
		OwnerID:    owner,
		Visibility: in.Visibility,
		Sort:       req.Sort[0],
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

	items, err := s.store.SelectShelves(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := page.Result[domain.Shelf]{Items: items}
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
