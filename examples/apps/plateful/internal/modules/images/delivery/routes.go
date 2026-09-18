// Package delivery is the images module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/images/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start image-routes

// Register adds the images routes to r, under an organisation. Every route
// requires a signed-in member of the organisation in the path whose role
// grants the permission its guard names (guard.OrgMember): anyone else gets
// 404 org_not_found, as if the organisation didn't exist.
//
// Three of the four routes are the upload, split in two because the bytes
// never pass through the app. POST /images hands out a signed PUT, the
// client sends the file straight to the bucket with it, and POST
// /images/{id}/confirm tells the app to go and look. The app has no upload
// endpoint of its own: there is no multipart handler here, because
// storage.Store offers no signed POST to build one against, and streaming
// every restaurant's photos through the API server to put them in a bucket
// the client can already reach would be work for nothing.
//
// The confirmation is a POST rather than a PATCH because it isn't an edit:
// the client sends no fields at all, it only says "look now", and what the
// image becomes is decided by the object in the bucket. Being a POST, it
// also passes through the app's idempotency middleware, so a client that
// retries with the same Idempotency-Key gets the first answer back instead
// of a second Stat.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	images := r.Group("/v1/orgs/{orgId}/images", gorbital.Tags("Images"))

	gorbital.Post(images, "", h.requestUpload, gorbital.OperationID("images-request-upload"),
		gorbital.Summary("Ask for somewhere to upload an image"),
		gorbital.Description("Creates a pending image and returns `upload_url`, a signed URL that accepts one `PUT` of the file until `upload_expires_at`. Send the bytes there with the same `Content-Type`, then call the confirm route. The URL accepts `PUT` only: there is no upload form and no multipart endpoint."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusUnprocessableEntity, http.StatusServiceUnavailable),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Post(images, "/{id}/confirm", h.confirmUpload, gorbital.OperationID("images-confirm-upload"),
		gorbital.Summary("Confirm an image was uploaded"),
		gorbital.Description("Looks the object up in the bucket and records its real size and media type. Answers 409 `image_not_uploaded` when nothing is there, and 422 when the file is larger than `images.max_bytes` or isn't an accepted image type."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(images, "/{id}", h.getImage, gorbital.OperationID("images-get"),
		gorbital.Summary("Get an image and a link to it"),
		gorbital.Description("Returns a signed download URL that expires in five minutes, so a link that leaks stops working almost at once. Read the image again for a fresh one."),
		gorbital.Errors(http.StatusNotFound, http.StatusServiceUnavailable),
		guard.OrgMember(usecase.PermRead))
	gorbital.Delete(images, "/{id}", h.deleteImage, gorbital.OperationID("images-delete"),
		gorbital.Summary("Delete an image"),
		gorbital.Description("Removes the file from the bucket and the image from the organisation."),
		gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound, http.StatusServiceUnavailable),
		guard.OrgMember(usecase.PermWrite))
}

// docs:end image-routes
