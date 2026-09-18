package domain

import (
	"errors"
	"strings"
)

// Errors of the images use cases. module.go maps each to an HTTP status and
// a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("images: a member of the organisation is required")

	// ErrInvalidImage reports invalid fields. The error is usually a
	// *ValidationError.
	ErrInvalidImage = errors.New("images: invalid image")

	// ErrImageNotFound reports an image that doesn't exist, or that belongs
	// to another organisation. The cases aren't told apart, so IDs can't be
	// probed.
	ErrImageNotFound = errors.New("images: image not found")

	// ErrImageNotUploaded reports a confirmation of an image whose object
	// isn't in the bucket: the client never sent the bytes, sent them to a
	// different key, or the signed URL expired first.
	ErrImageNotUploaded = errors.New("images: nothing was uploaded for this image")

	// ErrImageTooLarge reports an object past the images.max_bytes setting.
	// It can only be found after the upload: a signed PUT carries no size
	// limit of its own.
	ErrImageTooLarge = errors.New("images: the uploaded file is too large")

	// ErrUnsupportedImageType reports a media type the platform doesn't
	// accept. The store reports what the uploader claimed, so this says the
	// claim is one we refuse, not that the bytes were inspected.
	ErrUnsupportedImageType = errors.New("images: unsupported image type")

	// ErrStorageUnavailable reports an app with no file storage configured
	// at all (a nil gorbital.Deps.Storage). The module can still be mounted
	// and its OpenAPI document exported; nothing can be uploaded.
	ErrStorageUnavailable = errors.New("images: file storage is not configured")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of an upload request.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "images: invalid image: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidImage.
func (e *ValidationError) Unwrap() error { return ErrInvalidImage }
