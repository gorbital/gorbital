package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/menus/domain"
)

// listOptions are the page sizes and sorts ListItems accepts.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"name", "price_minor", "created_at"},
		DefaultSort: []page.SortField{{Field: "name"}},
	}
}

// ListItemsInput selects a page of an organisation's menu items.
type ListItemsInput struct {
	Page page.Params
	// Section keeps items printed under this heading, ignoring case.
	Section string
	// Available, when set, keeps the items that are or aren't available.
	Available *bool
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort   string    `json:"s"`
	Time   time.Time `json:"t,omitzero"`
	Text   string    `json:"x,omitempty"`
	Number int64     `json:"n,omitempty"`
	ID     string    `json:"i"`
}

// docs:start list-items

// ListItems returns a page of the organisation orgID's menu items, by name
// unless the request sorts otherwise. This is the staff's view of the menu:
// a dish the kitchen has switched off is still theirs to see and to switch
// back on, so nothing is hidden here.
//
// The page is a keyset cursor, not an offset: the cursor records the last
// row's sort value and ID, and the next page asks for the rows after that
// pair. A restaurant reorders and reprices its menu all day, and an offset
// would skip or repeat dishes as rows move underneath it.
//
// A cursor is opaque but it is not signed, and it says nothing about who may
// read the next page. Every filter that decides which rows exist for this
// caller — the organisation above all — is applied again to each page from
// the request, never taken from the cursor.
func (s *Service) ListItems(ctx context.Context, orgID string, in ListItemsInput) (page.Result[domain.Item], error) {
	var none page.Result[domain.Item]
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

	q := ListQuery{
		OrgID:     orgID,
		Section:   in.Section,
		Available: in.Available,
		Sort:      req.Sort[0],
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
		q.After = &Position{Time: c.Time, Text: c.Text, Number: c.Number, ID: c.ID}
	}

	items, err := s.store.SelectItems(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := page.Result[domain.Item]{Items: items}
	if len(items) > req.Limit {
		res.Items = items[:req.Limit]
		last := res.Items[len(res.Items)-1]
		c := cursor{Sort: sort, ID: last.ID}
		switch q.Sort.Field {
		case "created_at":
			c.Time = last.CreatedAt
		case "name":
			c.Text = last.Name
		case "price_minor":
			c.Number = last.PriceMinor
		}
		if res.NextCursor, err = page.EncodeCursor(c); err != nil {
			return none, err
		}
	}
	return res, nil
}

// docs:end list-items
