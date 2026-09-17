package delivery

import "context"

func (h handlers) deleteInvoice(ctx context.Context, in *invoiceIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteInvoice(ctx, in.OrgID, in.ID)
}
