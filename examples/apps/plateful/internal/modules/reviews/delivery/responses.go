package delivery

import (
	"errors"
	"math"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/reviews/domain"
)

// docs:start review-responses

// ReviewResponse is a review as the API returns it.
//
// There is no customer field, and adding one would be a privacy bug rather
// than a feature: the public list is unauthenticated, so any account ID in
// it would be readable by anyone who asked. A diner needs the stars, the
// words and the date, and nothing else identifies the person who wrote them.
type ReviewResponse struct {
	ID        string    `json:"id" example:"rev_mfrggzdfmztwq2lkmfrggzdfmy"`
	Rating    int       `json:"rating" minimum:"1" maximum:"5"`
	Comment   string    `json:"comment"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// docs:end review-responses

type reviewOutput struct {
	Body ReviewResponse
}

func toResponse(r domain.Review) ReviewResponse {
	return ReviewResponse{
		ID:        r.ID,
		Rating:    r.Rating,
		Comment:   r.Comment,
		Version:   r.Version,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// ReviewPage is a page of a restaurant's reviews with what they add up to.
type ReviewPage struct {
	Items      []ReviewResponse `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
	// ReviewCount and AverageRating count only the reviews a diner can see:
	// a hidden one is in neither.
	ReviewCount   int     `json:"review_count" doc:"How many visible reviews the restaurant has"`
	AverageRating float64 `json:"average_rating" example:"4.5" doc:"The mean of those reviews, 0 when there are none"`
}

type reviewPageOutput struct {
	Body ReviewPage
}

// averageForWire rounds the exact average to two decimal places. The stored
// count and sum stay exact (domain.Rating.Average divides them on every
// read); this is only so that four stars out of three reviews reaches a
// client as 1.33 rather than seventeen digits of binary fraction.
func averageForWire(a domain.Rating) float64 {
	return math.Round(a.Average()*100) / 100
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the review is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
