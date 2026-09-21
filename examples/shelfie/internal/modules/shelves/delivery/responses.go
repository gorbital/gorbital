package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// ShelfResponse is a shelf as the API returns it.
type ShelfResponse struct {
	ID          string    `json:"id" example:"shl_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility" enum:"private,shared"`
	Version     int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type shelfOutput struct {
	Body ShelfResponse
}

// shelfIDInput is the path of the routes on one shelf.
type shelfIDInput struct {
	ID string `path:"id" maxLength:"64" example:"shl_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(shelf domain.Shelf) ShelfResponse {
	return ShelfResponse{
		ID:          shelf.ID,
		Name:        shelf.Name,
		Description: shelf.Description,
		Visibility:  string(shelf.Visibility),
		Version:     shelf.Version,
		CreatedAt:   shelf.CreatedAt,
		UpdatedAt:   shelf.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the shelf is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
