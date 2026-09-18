// Package domain holds the images module's images and their rules: what an
// image may be for, which media types the platform accepts, and how an
// object's key in the storage bucket is derived. It imports only the
// standard library, so none of this depends on how the bytes are stored.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Purpose is what an image is for. A restaurant's cover is the picture at
// the top of its page; a menu item photo belongs to one dish. The purpose
// is fixed when the upload is requested, because it decides which of the
// other modules may point at the image.
type Purpose string

// Purposes. The migration's CHECK constraint lists the same two.
const (
	PurposeRestaurantCover Purpose = "restaurant_cover"
	PurposeMenuItemPhoto   Purpose = "menu_item_photo"
)

// Valid reports whether v is a known purpose.
func (v Purpose) Valid() bool {
	switch v {
	case PurposeRestaurantCover, PurposeMenuItemPhoto:
		return true
	}
	return false
}

// Status is how far an image has got. An image is pending from the moment
// its signed upload URL is handed out, and ready once the object behind it
// has been found in the bucket, measured and accepted. Nothing moves it
// back: a replacement is a new image.
type Status string

// Statuses. The migration's CHECK constraint lists the same two.
const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
)

// contentTypes are the media types the platform accepts, each with the
// extension its object key gets. The extension matters because an object
// store has no types of its own: a driver that loses the metadata guesses
// the type back from the key, and every driver serves the key's basename to
// whoever downloads it.
var contentTypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
	"image/avif": "avif",
}

// AllowedContentTypes lists the media types the platform accepts, sorted,
// for error messages and the OpenAPI schema.
func AllowedContentTypes() []string {
	return []string{"image/avif", "image/jpeg", "image/png", "image/webp"}
}

// NormaliseContentType returns the media type without its parameters and in
// lower case, as a store reports it: "IMAGE/JPEG; charset=binary" and
// "image/jpeg" are the same type, and only the first part is ours to check.
func NormaliseContentType(contentType string) string {
	base, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// AllowedContentType reports whether contentType is one the platform
// accepts, ignoring its parameters and case.
func AllowedContentType(contentType string) bool {
	_, ok := contentTypes[NormaliseContentType(contentType)]
	return ok
}

// extensionFor returns the file extension for an accepted media type.
func extensionFor(contentType string) (string, bool) {
	ext, ok := contentTypes[NormaliseContentType(contentType)]
	return ext, ok
}

// docs:start image-storage-key

// StorageKey returns the key the image's object has in the bucket:
// orgs/{orgID}/images/{imageID}.{ext}.
//
// The key is derived, never taken from the request. The organisation comes
// first, so every object a restaurant owns sits under a prefix that names
// that restaurant's organisation and nothing else: a key one organisation
// can produce can never name another organisation's object, whatever a
// client sends. The image's own ID is unguessable, so the key doesn't leak
// the file's original name either. The extension follows the media type,
// because an object store keeps no types of its own that a driver can be
// relied on to return.
func StorageKey(orgID, imageID, contentType string) (string, error) {
	ext, ok := extensionFor(contentType)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedImageType, contentType)
	}
	if orgID == "" || imageID == "" || strings.ContainsAny(orgID+imageID, "/.") {
		return "", fmt.Errorf("%w: org %q, image %q", ErrInvalidImage, orgID, imageID)
	}
	return "orgs/" + orgID + "/images/" + imageID + "." + ext, nil
}

// docs:end image-storage-key

// An Image is one uploaded file: the row that records where the bytes went
// and whether they ever arrived.
type Image struct {
	ID    string
	OrgID string
	// CreatedBy is the member who asked for the upload, for display and
	// audit. Access comes only from membership of OrgID.
	CreatedBy string
	Purpose   Purpose
	// StorageKey is the object's key in the bucket, from StorageKey.
	StorageKey string
	// ContentType is what was asked for while the image is pending, and
	// what the store reports once it is ready.
	ContentType string
	// SizeBytes is the object's size as the store measured it, 0 while the
	// image is pending.
	SizeBytes int64
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewImage returns a pending image of orgID created by createdBy, with the
// key its object will have. Nothing is uploaded yet: the caller hands out a
// signed PUT for Image.StorageKey and waits to be told the bytes arrived.
func NewImage(id, orgID, createdBy string, purpose Purpose, contentType string, now time.Time) (Image, error) {
	if !purpose.Valid() {
		return Image{}, &ValidationError{Errors: []FieldError{{
			Field:   "purpose",
			Message: "must be restaurant_cover or menu_item_photo",
		}}}
	}
	contentType = NormaliseContentType(contentType)
	if !AllowedContentType(contentType) {
		return Image{}, &ValidationError{Errors: []FieldError{{
			Field:   "content_type",
			Message: "must be one of " + strings.Join(AllowedContentTypes(), ", "),
		}}}
	}
	key, err := StorageKey(orgID, id, contentType)
	if err != nil {
		return Image{}, err
	}
	return Image{
		ID: id, OrgID: orgID, CreatedBy: createdBy, Purpose: purpose,
		StorageKey: key, ContentType: contentType, Status: StatusPending,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Confirm returns the image as the object in the bucket describes it:
// ready, with the size and media type the store measured rather than the
// ones the client asked for. maxBytes is the images.max_bytes setting.
//
// Both checks happen here, after the upload, because a signed PUT carries
// no conditions: the URL says which key may be written and until when, and
// nothing else. The client chooses the length and the Content-Type header,
// so the only honest moment to judge them is when the object exists.
func (i Image) Confirm(size int64, contentType string, maxBytes int64, now time.Time) (Image, error) {
	if maxBytes > 0 && size > maxBytes {
		return i, fmt.Errorf("%w: %d bytes, at most %d", ErrImageTooLarge, size, maxBytes)
	}
	contentType = NormaliseContentType(contentType)
	if !AllowedContentType(contentType) {
		return i, fmt.Errorf("%w: %s", ErrUnsupportedImageType, contentType)
	}
	next := i
	next.SizeBytes, next.ContentType, next.Status, next.UpdatedAt = size, contentType, StatusReady, now
	return next, nil
}
