package delivery

import "context"

// getCourier returns the caller's own courier profile. The operation takes
// no input at all: "me" is the account in the actor, and the only courier a
// courier may read.
func (h handlers) getCourier(ctx context.Context, _ *struct{}) (*courierOutput, error) {
	c, err := h.svc.GetCourier(ctx)
	if err != nil {
		return nil, err
	}
	return &courierOutput{Body: toResponse(c)}, nil
}
