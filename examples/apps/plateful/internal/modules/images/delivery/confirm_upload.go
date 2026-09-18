package delivery

import "context"

// confirmUpload takes no body: everything it records comes from the object
// in the bucket, not from the client.
func (h handlers) confirmUpload(ctx context.Context, in *imageIDInput) (*imageOutput, error) {
	image, err := h.svc.ConfirmUpload(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &imageOutput{Body: toResponse(image)}, nil
}
