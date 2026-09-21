package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

// ClubBookResponse is a club book as the API returns it.
type ClubBookResponse struct {
	ID        string    `json:"id" example:"clb_mfrggzdfmztwq2lkmfrggzdfmy"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Status    string    `json:"status" enum:"proposed,reading,finished"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type clubBookOutput struct {
	Body ClubBookResponse
}

// clubBookIDInput is the path of the routes on one club book.
type clubBookIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"clb_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(clubBook domain.ClubBook) ClubBookResponse {
	return ClubBookResponse{
		ID:        clubBook.ID,
		Title:     clubBook.Title,
		Author:    clubBook.Author,
		Status:    string(clubBook.Status),
		Note:      clubBook.Note,
		CreatedBy: clubBook.CreatedBy,
		Version:   clubBook.Version,
		CreatedAt: clubBook.CreatedAt,
		UpdatedAt: clubBook.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the club book is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
