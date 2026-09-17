package delivery

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
)

type createInvoiceInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Number   string `json:"number" minLength:"1" maxLength:"100" doc:"Unique in the organisation, ignoring case"`
		Customer string `json:"customer" minLength:"1" maxLength:"100"`
		Status   string `json:"status,omitempty" enum:"draft,sent,paid,void" default:"draft"`
		Note     string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) createInvoice(ctx context.Context, in *createInvoiceInput) (*invoiceOutput, error) {
	invoice, err := h.svc.CreateInvoice(ctx, in.OrgID, domain.InvoiceFields{
		Number:   in.Body.Number,
		Customer: in.Body.Customer,
		Status:   domain.Status(in.Body.Status),
		Note:     in.Body.Note,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &invoiceOutput{Body: toResponse(invoice)}, nil
}
