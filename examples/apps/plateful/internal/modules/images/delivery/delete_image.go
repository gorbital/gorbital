package delivery

import "context"

func (h handlers) deleteImage(ctx context.Context, in *imageIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteImage(ctx, in.OrgID, in.ID)
}
