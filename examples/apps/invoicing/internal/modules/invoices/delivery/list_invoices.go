package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/invoicing/internal/modules/invoices/domain"
	"example.com/invoicing/internal/modules/invoices/usecase"
)

type listInvoicesInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status string `query:"status" enum:"draft,sent,paid,void" doc:"Only invoices with this status"`
}

// InvoicePage is a page of invoices.
type InvoicePage struct {
	Items      []InvoiceResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type invoicePageOutput struct {
	Body InvoicePage
}

func (h handlers) listInvoices(ctx context.Context, in *listInvoicesInput) (*invoicePageOutput, error) {
	res, err := h.svc.ListInvoices(ctx, in.OrgID, usecase.ListInvoicesInput{
		Page:   in.Params,
		Status: domain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &invoicePageOutput{Body: InvoicePage{Items: make([]InvoiceResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, invoice := range res.Items {
		out.Body.Items[i] = toResponse(invoice)
	}
	return out, nil
}
