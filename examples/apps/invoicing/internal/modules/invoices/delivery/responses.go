package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// InvoiceResponse is an invoice as the API returns it.
type InvoiceResponse struct {
	ID        string    `json:"id" example:"inv_mfrggzdfmztwq2lkmfrggzdfmy"`
	Number    string    `json:"number"`
	Customer  string    `json:"customer"`
	Status    string    `json:"status" enum:"draft,sent,paid,void"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type invoiceOutput struct {
	Body InvoiceResponse
}

// invoiceIDInput is the path of the routes on one invoice.
type invoiceIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"inv_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(invoice domain.Invoice) InvoiceResponse {
	return InvoiceResponse{
		ID:        invoice.ID,
		Number:    invoice.Number,
		Customer:  invoice.Customer,
		Status:    string(invoice.Status),
		Note:      invoice.Note,
		CreatedBy: invoice.CreatedBy,
		Version:   invoice.Version,
		CreatedAt: invoice.CreatedAt,
		UpdatedAt: invoice.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the invoice is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
