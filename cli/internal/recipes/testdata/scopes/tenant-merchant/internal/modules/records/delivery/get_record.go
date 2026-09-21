package delivery

import "context"

func (h handlers) getRecord(ctx context.Context, in *recordIDInput) (*recordOutput, error) {
	record, err := h.svc.GetRecord(ctx, in.MerchantID, in.ID)
	if err != nil {
		return nil, err
	}
	return &recordOutput{Body: toResponse(record)}, nil
}
