package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/images/domain"
	"example.com/plateful/internal/modules/images/usecase"
)

// ImageResponse is an image as the API returns it. The object's key is
// never among the fields: it is derived from the organisation and the ID,
// and a client that can't see it can't try to substitute another one.
type ImageResponse struct {
	ID          string    `json:"id" example:"img_mfrggzdfmztwq2lkmfrggzdfmy"`
	Purpose     string    `json:"purpose" enum:"restaurant_cover,menu_item_photo"`
	Status      string    `json:"status" enum:"pending,ready" doc:"pending until the upload is confirmed"`
	ContentType string    `json:"content_type" example:"image/jpeg" doc:"What the store reports for the object once the upload is confirmed"`
	SizeBytes   int64     `json:"size_bytes" doc:"The object's size as the store measured it, 0 while pending"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type imageOutput struct {
	Body ImageResponse
}

func toResponse(i domain.Image) ImageResponse {
	return ImageResponse{
		ID:          i.ID,
		Purpose:     string(i.Purpose),
		Status:      string(i.Status),
		ContentType: i.ContentType,
		SizeBytes:   i.SizeBytes,
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
	}
}

// UploadResponse is a pending image and where to send its bytes.
type UploadResponse struct {
	ID      string `json:"id" example:"img_mfrggzdfmztwq2lkmfrggzdfmy"`
	Purpose string `json:"purpose" enum:"restaurant_cover,menu_item_photo"`
	Status  string `json:"status" enum:"pending,ready"`
	// UploadURL accepts one PUT of the file. It is a bearer token for that
	// one object: anyone holding it can write there until it expires.
	UploadURL       string    `json:"upload_url" doc:"Send the file here with a single PUT and the Content-Type you asked for"`
	UploadExpiresAt time.Time `json:"upload_expires_at"`
}

type uploadOutput struct {
	Body UploadResponse
}

func toUploadResponse(u usecase.Upload) UploadResponse {
	return UploadResponse{
		ID:              u.Image.ID,
		Purpose:         string(u.Image.Purpose),
		Status:          string(u.Image.Status),
		UploadURL:       u.URL,
		UploadExpiresAt: u.ExpiresAt,
	}
}

// ViewResponse is an image and a short-lived link to the file.
type ViewResponse struct {
	ID      string `json:"id" example:"img_mfrggzdfmztwq2lkmfrggzdfmy"`
	Purpose string `json:"purpose" enum:"restaurant_cover,menu_item_photo"`
	Status  string `json:"status" enum:"pending,ready"`
	// URL downloads the file. A pending image has one too, and it answers
	// 404 until the object exists.
	URL       string    `json:"url" doc:"A signed download link, good until expires_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type viewOutput struct {
	Body ViewResponse
}

func toViewResponse(v usecase.View) ViewResponse {
	return ViewResponse{
		ID:        v.Image.ID,
		Purpose:   string(v.Image.Purpose),
		Status:    string(v.Image.Status),
		URL:       v.URL,
		ExpiresAt: v.ExpiresAt,
	}
}

// imageIDInput is an organisation's image named in the path.
type imageIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"img_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the image request is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
