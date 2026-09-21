package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
)

// listOptions are the page sizes and sorts ListRecords accepts.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"created_at", "updated_at", "title"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
}

// ListRecordsInput selects a page of a merchant's records.
type ListRecordsInput struct {
	Page page.Params
	// State keeps records with this state; empty keeps all.
	State domain.State
}

// cursor is the position a page ended at, with the sort it belongs to, so a
// cursor can't be used with a different sort.
type cursor struct {
	Sort string    `json:"s"`
	Time time.Time `json:"t,omitzero"`
	Text string    `json:"x,omitempty"`
	ID   string    `json:"i"`
}

// ListRecords returns a page of the merchant merchantID's records, sorted by
// one field (newest first by default).
func (s *Service) ListRecords(ctx context.Context, merchantID string, in ListRecordsInput) (page.Result[domain.Record], error) {
	var none page.Result[domain.Record]
	if _, err := memberID(ctx, merchantID); err != nil {
		return none, err
	}
	req, err := in.Page.Request(listOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 {
		return none, fmt.Errorf("%w: sort by one field", page.ErrInvalidSort)
	}
	if in.State != "" && !in.State.Valid() {
		return none, &domain.ValidationError{Errors: []domain.FieldError{{Field: "state", Message: "must be open or done"}}}
	}

	q := ListQuery{
		MerchantID: merchantID,
		State:      in.State,
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

	items, err := s.store.SelectRecords(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := page.Result[domain.Record]{Items: items}
	if len(items) > req.Limit {
		res.Items = items[:req.Limit]
		last := res.Items[len(res.Items)-1]
		c := cursor{Sort: sort, ID: last.ID}
		switch q.Sort.Field {
		case "created_at":
			c.Time = last.CreatedAt
		case "updated_at":
			c.Time = last.UpdatedAt
		case "title":
			c.Text = last.Title
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
