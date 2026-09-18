// Package images is Plateful's images module: the cover photos and dish
// photos a restaurant's staff upload, stored in the app's file storage
// (gorbital.dev/modules/storage) and recorded in the images table, in four
// layers (domain, usecase, repository, delivery) with one file per
// operation in each.
//
// The files never pass through the app. A client asks for an upload, gets a
// signed PUT URL, sends the bytes to the bucket itself and comes back to
// confirm; the app then asks the store what arrived. That shape is not a
// preference, it is what storage.Store supports: it signs GET and PUT URLs
// and nothing else, so there is no signed POST to build a browser upload
// form against, and no multipart helper to lean on.
package images

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules/images/delivery"
	"example.com/plateful/internal/modules/images/domain"
	"example.com/plateful/internal/modules/images/repository"
	"example.com/plateful/internal/modules/images/usecase"
)

// Module returns the images module. main.go adds it with every other module
// through modules.All; its routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes, permission
// names, audit action names and setting keys are public API: add new ones,
// never change existing ones.
func Module() gorbital.Module {
	// Declared in Settings, before the stores exist, and used in Routes: no
	// lookup by key.
	var maxBytes *settings.Setting[int]
	return gorbital.Module{
		Name: "images",
		// docs:start image-errors
		// guard.OrgMember answers org_not_found for an organisation the
		// caller isn't a member of, and forbidden for a role without the
		// permission. The three below it are what an upload the app never
		// saw can turn out to be, and each says which half of the flow went
		// wrong: nothing arrived, too much arrived, or the wrong kind.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidImage, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the image request is not valid"},
			{Err: domain.ErrImageNotFound, Status: http.StatusNotFound, Code: "image_not_found", Detail: "the organisation has no image with this ID"},
			{Err: domain.ErrImageNotUploaded, Status: http.StatusConflict, Code: "image_not_uploaded", Detail: "no file has been uploaded for this image yet; PUT it to the upload URL first"},
			{Err: domain.ErrImageTooLarge, Status: http.StatusUnprocessableEntity, Code: "image_too_large", Detail: "the uploaded file is larger than the images.max_bytes setting allows"},
			{Err: domain.ErrUnsupportedImageType, Status: http.StatusUnprocessableEntity, Code: "unsupported_image_type", Detail: "the file is not one of the accepted image types"},
			// The honest answer when an app has no file storage: this
			// module cannot work, and says so, rather than failing later
			// with something that looks like a bug.
			{Err: domain.ErrStorageUnavailable, Status: http.StatusServiceUnavailable, Code: "storage_unavailable", Detail: "this app has no file storage configured, so images cannot be stored"},
		},
		// docs:end image-errors
		// docs:start image-permissions
		// Organisation permissions: every member holds them through their
		// role in the restaurant's organisation, an API key only when its
		// scopes include them. Platform roles grant nothing in an
		// organisation. Requesting an upload is a write, not a read: it
		// hands out a URL that can put bytes in the bucket.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's images and get links to them", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Upload, confirm and delete the organisation's images", OrgRoles: []string{"owner", "admin"}},
		},
		// docs:end image-permissions
		// docs:start image-settings
		// The limit an operator can change without a deploy. It is enforced
		// at confirmation rather than at the door, because a signed PUT URL
		// carries no maximum length: by the time the app can measure the
		// file, the file is already in the bucket.
		Settings: func(r *settings.Registry) {
			maxBytes = settings.Int(r, "images.max_bytes", 5<<20,
				settings.Describe("The largest image file the platform accepts, in bytes. Checked when an upload is confirmed, not while it is running: signed upload URLs carry no size limit, so a larger file uploads successfully and is then refused with 422 image_too_large, and its bytes sit in the bucket until the image is deleted."),
				settings.Group("images"),
				settings.Range(64<<10, 25<<20),
				settings.ReasonRequired(),
			)
		},
		// docs:end image-settings
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs. d.Storage is also nil in an
			// app with no file storage at all, which the service answers
			// with storage_unavailable rather than a panic.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Storage, d.Audit, d.Logger, maxBytes)
			delivery.Register(r, svc)
		},
	}
}
