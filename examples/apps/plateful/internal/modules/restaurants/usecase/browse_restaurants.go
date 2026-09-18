package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// browseOptions are the page sizes and sorts BrowseRestaurants accepts.
func browseOptions() page.Options {
	return page.Options{
		SortFields:  []string{"name", "created_at"},
		DefaultSort: []page.SortField{{Field: "name"}},
	}
}

// BrowseInput selects a page of restaurants.
type BrowseInput struct {
	Page page.Params
	// Cuisine keeps restaurants of this cuisine, ignoring case.
	Cuisine string
	// All asks for every restaurant whatever its status, which only
	// platform staff may do (PermOversee).
	All bool
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort string `json:"s"`
	Time string `json:"t,omitempty"`
	Text string `json:"x,omitempty"`
	ID   string `json:"i"`
}

// docs:start browse-restaurants

// BrowseRestaurants returns a page of restaurants: the open ones for a
// customer, and every one of them for platform staff (in.All, which the
// route allows only with PermOversee).
//
// The filter that decides which rows exist for this caller is applied to
// every page, not stored in the cursor. A page cursor is opaque but it is
// not signed: it says where the last page ended and nothing about who may
// read the next one, so a customer who pastes a cursor from somewhere else
// still sees only open restaurants.
func (s *Service) BrowseRestaurants(ctx context.Context, in BrowseInput) (page.Result[domain.Restaurant], error) {
	var none page.Result[domain.Restaurant]
	if _, err := callerID(ctx); err != nil {
		return none, err
	}
	req, err := in.Page.Request(browseOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 {
		return none, fmt.Errorf("%w: sort by one field", page.ErrInvalidSort)
	}

	q := BrowseQuery{
		Cuisine: in.Cuisine,
		Sort:    req.Sort[0],
		// One more than the limit shows whether there is a next page.
		Limit: req.Limit + 1,
	}
	if !in.All {
		q.Statuses = []domain.Status{domain.StatusOpen}
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
		q.After = &Position{Time: c.Time, Text: c.Text, ID: c.ID, IsSet: true}
	}

	items, err := s.store.SelectRestaurants(ctx, q)
	if err != nil {
		return none, storeError("browse", err)
	}
	res := page.Result[domain.Restaurant]{Items: items}
	if len(items) > req.Limit {
		res.Items = items[:req.Limit]
		last := res.Items[len(res.Items)-1]
		c := cursor{Sort: sort, ID: last.ID}
		switch q.Sort.Field {
		case "created_at":
			c.Time = last.CreatedAt.Format(time.RFC3339Nano)
		case "name":
			c.Text = last.Name
		}
		if res.NextCursor, err = page.EncodeCursor(c); err != nil {
			return none, err
		}
	}
	if !in.All {
		for i := range res.Items {
			res.Items[i].SuspendedReason = ""
		}
	}
	return res, nil
}

// docs:end browse-restaurants

func sortName(f page.SortField) string {
	if f.Desc {
		return "-" + f.Field
	}
	return f.Field
}
