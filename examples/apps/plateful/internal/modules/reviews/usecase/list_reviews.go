package usecase

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/reviews/domain"
)

// listOptions are the page sizes and sorts ListReviews accepts. There is one
// sort: a review list is read newest first, and letting a diner reorder it
// by rating would only help a restaurant screenshot its best page.
func listOptions() page.Options {
	return page.Options{
		SortFields:  []string{"created_at"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
}

// ListInput selects a page of one restaurant's reviews.
type ListInput struct {
	RestaurantID string
	Page         page.Params
}

// ListResult is a page of reviews with the rating the restaurant's visible
// reviews add up to.
type ListResult struct {
	Page   page.Result[domain.Review]
	Rating domain.Rating
}

// cursor is the position a page ended at.
type cursor struct {
	Time string `json:"t"`
	ID   string `json:"i"`
}

// docs:start list-reviews

// ListReviews returns a page of a restaurant's visible reviews, newest
// first, with the count and the average of all of them.
//
// This is the one operation in Plateful that takes no actor. There is no
// callerID call above, and that absence is the whole design: a diner
// deciding where to eat tonight has not signed in, will not sign in to read
// a review list, and would simply go to a competitor's app if the list asked
// them to. The route says guard.Public(), and everything this function
// returns is written on the assumption that a stranger is reading it.
//
// Two consequences. Hidden reviews are filtered in the query rather than
// after it, so a moderated review is not merely absent from the page but
// absent from the count and from the average as well. And the customer's
// account ID never leaves this layer: the repository reads it, the response
// type has no field for it, and a diner learns what was said and not who
// said it.
//
// The cursor is opaque but unsigned, exactly as in the restaurants module's
// browse. It says where the last page ended and nothing about who may read
// the next one, which is fine here because the answer to "who may read this"
// is "anybody".
func (s *Service) ListReviews(ctx context.Context, in ListInput) (ListResult, error) {
	var none ListResult
	req, err := in.Page.Request(listOptions())
	if err != nil {
		return none, err
	}
	if len(req.Sort) != 1 || req.Sort[0].Field != "created_at" || !req.Sort[0].Desc {
		return none, fmt.Errorf("%w: reviews are listed newest first", page.ErrInvalidSort)
	}

	q := ListQuery{
		RestaurantID: in.RestaurantID,
		// One more than the limit shows whether there is a next page.
		Limit: req.Limit + 1,
	}
	if req.Cursor != "" {
		var c cursor
		if err := page.DecodeCursor(req.Cursor, &c); err != nil {
			return none, err
		}
		if c.ID == "" || c.Time == "" {
			return none, fmt.Errorf("%w: it names no position", page.ErrInvalidCursor)
		}
		q.After = &Position{Time: c.Time, ID: c.ID}
	}

	items, err := s.store.SelectReviews(ctx, q)
	if err != nil {
		return none, storeError("list", err)
	}
	res := ListResult{Page: page.Result[domain.Review]{Items: items}}
	if len(items) > req.Limit {
		res.Page.Items = items[:req.Limit]
		last := res.Page.Items[len(res.Page.Items)-1]
		c := cursor{Time: last.CreatedAt.Format(time.RFC3339Nano), ID: last.ID}
		if res.Page.NextCursor, err = page.EncodeCursor(c); err != nil {
			return none, err
		}
	}
	if res.Rating, err = s.store.SelectRating(ctx, in.RestaurantID); err != nil {
		return none, storeError("list", err)
	}
	return res, nil
}

// docs:end list-reviews
