package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/app/internal/modules/records/domain"
)

// RecordResponse is a record as the API returns it.
type RecordResponse struct {
	ID        string    `json:"id" example:"rcr_mfrggzdfmztwq2lkmfrggzdfmy"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	State     string    `json:"state" enum:"open,done"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type recordOutput struct {
	Body RecordResponse
}

// recordIDInput is the path of the routes on one record.
type recordIDInput struct {
	MerchantID string `path:"merchantId" maxLength:"64" example:"mrc_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID         string `path:"id" maxLength:"64" example:"rcr_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(record domain.Record) RecordResponse {
	return RecordResponse{
		ID:        record.ID,
		Title:     record.Title,
		Note:      record.Note,
		State:     string(record.State),
		CreatedBy: record.CreatedBy,
		Version:   record.Version,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the record is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
