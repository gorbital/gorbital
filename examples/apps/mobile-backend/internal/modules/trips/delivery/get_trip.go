package delivery

import "context"

func (h handlers) getTrip(ctx context.Context, in *tripIDInput) (*tripOutput, error) {
	t, err := h.svc.GetTrip(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &tripOutput{Body: toResponse(t)}, nil
}
