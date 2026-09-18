package delivery

import (
	"context"

	"example.com/plateful/internal/modules/images/domain"
	"example.com/plateful/internal/modules/images/usecase"
)

// docs:start request-upload-handler

type requestUploadInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Purpose string `json:"purpose" enum:"restaurant_cover,menu_item_photo" doc:"What the image is for"`
		// ContentType is what the client says it is about to upload. The
		// enum keeps obvious mistakes out of the OpenAPI contract and gives
		// the object key its extension, but it is still only a claim: the
		// same list is checked again in the confirm route, against what the
		// store reports for the object that actually arrived.
		ContentType string `json:"content_type" enum:"image/jpeg,image/png,image/webp,image/avif" example:"image/jpeg"`
	}
}

// requestUpload is the whole handler: read the request, call the use case,
// shape the answer. The file is not here and never will be — this endpoint
// accepts JSON and answers with a URL, and the bytes go from the client
// straight to the bucket, which is why the handler stays this small.
func (h handlers) requestUpload(ctx context.Context, in *requestUploadInput) (*uploadOutput, error) {
	upload, err := h.svc.RequestUpload(ctx, in.OrgID, usecase.RequestUploadInput{
		Purpose:     domain.Purpose(in.Body.Purpose),
		ContentType: in.Body.ContentType,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &uploadOutput{Body: toUploadResponse(upload)}, nil
}

// docs:end request-upload-handler
