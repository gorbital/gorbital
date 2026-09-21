package delivery

import "context"

func (h handlers) deleteRecord(ctx context.Context, in *recordIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteRecord(ctx, in.MerchantID, in.ID)
}
