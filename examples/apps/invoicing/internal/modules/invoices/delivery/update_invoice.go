package delivery

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
	"example.com/invoicing/internal/modules/invoices/usecase"
)

type updateInvoiceInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"inv_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version  int64   `json:"version" minimum:"1" doc:"The version you read. If the invoice changed since, the update fails with invoice_version_conflict."`
		Number   *string `json:"number,omitempty" minLength:"1" maxLength:"100"`
		Customer *string `json:"customer,omitempty" minLength:"1" maxLength:"100"`
		Status   *string `json:"status,omitempty" enum:"draft,sent,paid,void"`
		Note     *string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) updateInvoice(ctx context.Context, in *updateInvoiceInput) (*invoiceOutput, error) {
	changes := domain.Changes{
		Number:   in.Body.Number,
		Customer: in.Body.Customer,
		Note:     in.Body.Note,
	}
	if in.Body.Status != nil {
		value := domain.Status(*in.Body.Status)
		changes.Status = &value
	}
	invoice, err := h.svc.UpdateInvoice(ctx, in.OrgID, in.ID, usecase.UpdateInvoiceInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &invoiceOutput{Body: toResponse(invoice)}, nil
}
