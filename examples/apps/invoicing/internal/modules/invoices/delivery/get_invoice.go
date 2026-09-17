package delivery

import "context"

func (h handlers) getInvoice(ctx context.Context, in *invoiceIDInput) (*invoiceOutput, error) {
	invoice, err := h.svc.GetInvoice(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &invoiceOutput{Body: toResponse(invoice)}, nil
}
