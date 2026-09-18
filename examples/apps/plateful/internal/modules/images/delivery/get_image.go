package delivery

import "context"

func (h handlers) getImage(ctx context.Context, in *imageIDInput) (*viewOutput, error) {
	view, err := h.svc.GetImage(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &viewOutput{Body: toViewResponse(view)}, nil
}
