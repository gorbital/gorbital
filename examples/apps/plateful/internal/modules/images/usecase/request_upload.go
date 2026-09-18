package usecase

import (
	"context"
	"net/http"
	"time"

	"example.com/plateful/internal/modules/images/domain"
)

// RequestUploadInput is what a restaurant's staff say before uploading:
// what the image is for, and what they are about to send.
type RequestUploadInput struct {
	Purpose domain.Purpose
	// ContentType is the media type the client claims. It decides the
	// object key's extension, and is checked again after the upload against
	// what the store reports.
	ContentType string
}

// Upload is a pending image and the signed URL that accepts its bytes.
type Upload struct {
	Image domain.Image
	// URL accepts one PUT of the object, and nothing else: not a POST, not
	// a form, not a second key.
	URL string
	// ExpiresAt is when URL stops working.
	ExpiresAt time.Time
}

// docs:start request-upload-signed-put

// RequestUpload records a pending image for the organisation orgID and
// returns a signed PUT URL for its object.
//
// This is the whole reason the flow has three steps. storage.Store signs
// GET and PUT URLs and nothing else: there is no signed POST, no upload
// form, and no multipart helper anywhere in the package, so the browser
// cannot be handed a form that posts to the bucket. What it can be handed
// is one URL that accepts exactly one PUT of exactly one key until it
// expires, which is what this returns.
//
// The key is derived, not taken: domain.StorageKey builds
// orgs/{orgID}/images/{imageID}.{ext} from the organisation in the path,
// which guard.OrgMember has already proved the caller belongs to, and a
// random image ID. Nothing the client sends reaches the key, so no request
// can name another restaurant's object, and the signature covers that key
// alone.
//
// What this cannot do is bound the upload. A signed PUT carries the key,
// the method and an expiry; it carries no maximum length and no required
// Content-Type, because storage.Store.SignedURL takes none. The size and
// the type are therefore checked in ConfirmUpload, once there is an object
// to measure.
func (s *Service) RequestUpload(ctx context.Context, orgID string, in RequestUploadInput) (Upload, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return Upload{}, err
	}
	bucket, err := s.bucket()
	if err != nil {
		return Upload{}, err
	}
	image, err := domain.NewImage(s.newID(), orgID, member, in.Purpose, in.ContentType, s.clock())
	if err != nil {
		return Upload{}, storeError("request upload", err)
	}
	url, err := bucket.SignedURL(ctx, image.StorageKey, http.MethodPut, UploadExpiry)
	if err != nil {
		return Upload{}, storeError("sign upload", err)
	}
	// The row is written after the URL is signed: a signature that fails
	// leaves nothing behind, while a row whose object never arrives is an
	// ordinary pending image the client can retry or delete.
	image, err = s.store.InsertImage(ctx, image)
	if err != nil {
		return Upload{}, storeError("request upload", err)
	}
	s.audit(ctx, ActionCreated, image.ID, map[string]any{"purpose": string(image.Purpose), "content_type": image.ContentType})
	return Upload{Image: image, URL: url, ExpiresAt: s.clock().Add(UploadExpiry)}, nil
}

// docs:end request-upload-signed-put
