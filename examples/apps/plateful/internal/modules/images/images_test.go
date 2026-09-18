package images_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	"example.com/plateful/internal/modules/images"
	"example.com/plateful/internal/modules/images/usecase"
	orgshttp "example.com/plateful/internal/modules/orgs"
)

// These tests drive the images routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts and
// real uploads: the file is PUT to the signed URL the app handed out,
// through the same handler a browser would reach.

// maxUploadBytes raises APP_MAX_BODY_BYTES for the test app.
//
// gorbitaltest configures the local storage driver, whose signed URLs are
// served by the app's own mux under /storage/, so an upload passes through
// the app's body-limit middleware on the way to the bucket. That limit
// defaults to 1 MiB, well under the 5 MiB images.max_bytes allows, and it
// would refuse a perfectly legal photo with a 413 before this module saw
// anything. It is a local-driver gotcha only: with S3 the client PUTs
// straight to the bucket and never touches the app.
const maxUploadBytes = "16777216"

// newApp builds the app with sign-in, organisations and the images module
// for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.NewWithEnv(t, map[string]string{"APP_MAX_BODY_BYTES": maxUploadBytes},
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), images.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and an organisation of its own, the
// restaurant this account's staff run, and returns the client, the user ID
// and the organisation ID.
func signUp(t *testing.T, app *gorbitaltest.App, email, restaurant string) (*gorbitaltest.Client, string, string) {
	t.Helper()
	client, userID := app.SignUp(t, email)
	res := client.Post("/v1/orgs", map[string]any{"name": restaurant})
	res.AssertStatus(t, http.StatusCreated)
	var org struct {
		ID string `json:"id"`
	}
	res.JSON(t, &org)
	if org.ID == "" {
		t.Fatalf("POST /v1/orgs returned no ID: %s", res.Body)
	}
	return client, userID, org.ID
}

// collection is the path of the organisation orgID's images.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/images" }

// apiUpload is the answer to a request for an upload.
type apiUpload struct {
	ID              string    `json:"id"`
	Purpose         string    `json:"purpose"`
	Status          string    `json:"status"`
	UploadURL       string    `json:"upload_url"`
	UploadExpiresAt time.Time `json:"upload_expires_at"`
}

// apiImage is a confirmed image.
type apiImage struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	Status      string `json:"status"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// apiView is an image with a signed download link.
type apiView struct {
	ID        string    `json:"id"`
	Purpose   string    `json:"purpose"`
	Status    string    `json:"status"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// requestUpload asks for an upload of contentType for purpose.
func requestUpload(t *testing.T, client *gorbitaltest.Client, orgID, purpose, contentType string) apiUpload {
	t.Helper()
	res := client.Post(collection(orgID), map[string]any{"purpose": purpose, "content_type": contentType})
	res.AssertStatus(t, http.StatusCreated)
	var upload apiUpload
	res.JSON(t, &upload)
	return upload
}

// send replays a signed URL against the app itself. The URL the local
// driver signs names the address the app would listen on, and the test app
// listens nowhere: it is served by a handler. Taking the path and the query
// and sending them through the app's client puts the request through the
// same /storage/ handler a real client's PUT would reach, signature and
// all, which is as close to a real upload as a test gets without a socket.
func send(t *testing.T, app *gorbitaltest.App, method, signed, contentType string, body []byte) *gorbitaltest.Response {
	t.Helper()
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL %q: %v", signed, err)
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, u.RequestURI(), r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return app.Client().Do(req)
}

// pixel is a small file standing in for a photo. Nothing in the module
// inspects the bytes, so what they are doesn't matter; what matters is how
// many there are and what the uploader calls them.
var pixel = bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 64)

// docs:start test-upload-flow

func TestUploadConfirmAndDownloadAnImage(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com", "Ada's Diner")

	upload := requestUpload(t, ada, adaOrg, "restaurant_cover", "image/png")
	if !strings.HasPrefix(upload.ID, "img_") || upload.Status != "pending" || upload.Purpose != "restaurant_cover" {
		t.Errorf("upload = %+v, want an img_ ID, the purpose asked for and the pending status", upload)
	}
	if !upload.UploadExpiresAt.After(time.Now()) {
		t.Errorf("upload_expires_at = %s, want a time in the future", upload.UploadExpiresAt)
	}
	// The key is derived from the organisation and the image, never from
	// the client, and the extension follows the media type.
	wantKey := "/storage/orgs/" + adaOrg + "/images/" + upload.ID + ".png"
	if u, err := url.Parse(upload.UploadURL); err != nil || u.Path != wantKey || u.Query().Get("method") != http.MethodPut {
		t.Fatalf("upload_url = %q (%v), want a PUT signature for %s", upload.UploadURL, err, wantKey)
	}

	// Confirming before anything is uploaded is a conflict, not a 404: the
	// image exists, the file doesn't.
	ada.Post(collection(adaOrg)+"/"+upload.ID+"/confirm", nil).
		AssertProblem(t, http.StatusConflict, "image_not_uploaded")

	send(t, app, http.MethodPut, upload.UploadURL, "image/png", pixel).AssertStatus(t, http.StatusOK)

	res := ada.Post(collection(adaOrg)+"/"+upload.ID+"/confirm", nil)
	res.AssertStatus(t, http.StatusOK)
	var confirmed apiImage
	res.JSON(t, &confirmed)
	if confirmed.Status != "ready" || confirmed.SizeBytes != int64(len(pixel)) || confirmed.ContentType != "image/png" {
		t.Errorf("confirmed = %+v, want ready, %d bytes and image/png as the store measured them", confirmed, len(pixel))
	}

	var view apiView
	ada.Get(collection(adaOrg)+"/"+upload.ID).JSON(t, &view)
	if view.Status != "ready" || view.URL == "" {
		t.Fatalf("GET = %+v, want a ready image with a signed URL", view)
	}
	if left := time.Until(view.ExpiresAt); left <= 0 || left > usecase.ViewExpiry {
		t.Errorf("expires_at = %s, %s away; want at most %s", view.ExpiresAt, left, usecase.ViewExpiry)
	}
	download := send(t, app, http.MethodGet, view.URL, "", nil)
	download.AssertStatus(t, http.StatusOK)
	if !bytes.Equal(download.Body, pixel) {
		t.Errorf("the signed URL returned %d bytes, want the %d that were uploaded", len(download.Body), len(pixel))
	}

	ada.Delete(collection(adaOrg)+"/"+upload.ID).AssertStatus(t, http.StatusNoContent)
	ada.Get(collection(adaOrg)+"/"+upload.ID).AssertProblem(t, http.StatusNotFound, "image_not_found")
	// The file went with the row: the URL signed a moment ago now finds
	// nothing.
	send(t, app, http.MethodGet, view.URL, "", nil).AssertStatus(t, http.StatusNotFound)

	// Every event names the member and the organisation.
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_id, coalesce(org_id, '') FROM audit_events WHERE resource_type = 'image' AND resource_id = $1 ORDER BY id`, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action, actorID, orgID string
		if err := rows.Scan(&action, &actorID, &orgID); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action+" by "+actorID+" in "+orgID)
	}
	var want []string
	for _, action := range []string{usecase.ActionCreated, usecase.ActionConfirmed, usecase.ActionDeleted} {
		want = append(want, action+" by "+adaID+" in "+adaOrg)
	}
	if !slices.Equal(trail, want) {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}

// docs:end test-upload-flow

// TestImagesAreProtected checks deny by default and organisation isolation:
// an organisation someone isn't a member of doesn't exist for them, so they
// can't ask it for an upload URL either.
func TestImagesAreProtected(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com", "Ada's Diner")
	bob, _, bobOrg := signUp(t, app, "bob@example.com", "Bob's Bistro")
	upload := requestUpload(t, ada, adaOrg, "menu_item_photo", "image/jpeg")
	item := collection(adaOrg) + "/" + upload.ID

	app.Client().Post(collection(adaOrg), map[string]any{"purpose": "menu_item_photo", "content_type": "image/jpeg"}).
		AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"request an upload": bob.Post(collection(adaOrg), map[string]any{"purpose": "menu_item_photo", "content_type": "image/jpeg"}),
		"get":               bob.Get(item),
		"confirm":           bob.Post(item+"/confirm", nil),
		"delete":            bob.Delete(item),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}

	// Bob is a member of his own organisation, so the guard lets him in;
	// Ada's image still isn't his, and the row is read by organisation as
	// well as by ID.
	bobItem := collection(bobOrg) + "/" + upload.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "image_not_found")
	bob.Post(bobItem+"/confirm", nil).AssertProblem(t, http.StatusNotFound, "image_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "image_not_found")
}

func TestUploadRequestsAreValidated(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com", "Ada's Diner")

	for _, tt := range []struct {
		name string
		body map[string]any
	}{
		{"unknown purpose", map[string]any{"purpose": "billboard", "content_type": "image/jpeg"}},
		{"a type we don't accept", map[string]any{"purpose": "restaurant_cover", "content_type": "image/gif"}},
		{"not an image at all", map[string]any{"purpose": "restaurant_cover", "content_type": "application/pdf"}},
		{"nothing at all", map[string]any{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaOrg), tt.body).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
		})
	}
	ada.Get(collection(adaOrg)+"/img_doesnotexistatallxxxxxxxx").AssertProblem(t, http.StatusNotFound, "image_not_found")
}

// docs:start test-oversized-upload

// TestAnOversizedUploadIsRefusedAtConfirmation is the honest cost of signed
// PUT URLs: the signature carries no maximum length, so a file past
// images.max_bytes uploads perfectly happily and is only refused afterwards,
// with its bytes already in the bucket.
func TestAnOversizedUploadIsRefusedAtConfirmation(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com", "Ada's Diner")

	upload := requestUpload(t, ada, adaOrg, "restaurant_cover", "image/jpeg")
	tooBig := make([]byte, (5<<20)+1) // one byte past the images.max_bytes default
	send(t, app, http.MethodPut, upload.UploadURL, "image/jpeg", tooBig).AssertStatus(t, http.StatusOK)

	ada.Post(collection(adaOrg)+"/"+upload.ID+"/confirm", nil).
		AssertProblem(t, http.StatusUnprocessableEntity, "image_too_large")

	// The image stays pending, so the client can replace the object with a
	// smaller one and confirm again.
	var view apiView
	ada.Get(collection(adaOrg)+"/"+upload.ID).JSON(t, &view)
	if view.Status != "pending" {
		t.Errorf("status after a refused confirmation = %q, want pending", view.Status)
	}
	send(t, app, http.MethodPut, upload.UploadURL, "image/jpeg", pixel).AssertStatus(t, http.StatusOK)
	res := ada.Post(collection(adaOrg)+"/"+upload.ID+"/confirm", nil)
	res.AssertStatus(t, http.StatusOK)
	var confirmed apiImage
	res.JSON(t, &confirmed)
	if confirmed.Status != "ready" || confirmed.SizeBytes != int64(len(pixel)) {
		t.Errorf("confirmed after replacing the file = %+v, want ready and %d bytes", confirmed, len(pixel))
	}
}

// docs:end test-oversized-upload

// TestAnUploadedTypeIsCheckedAgain shows what the store's content type is
// worth: it is whatever the uploader put in its PUT, kept as metadata.
// Nothing sniffs the bytes, so this refuses a claim, not a file.
func TestAnUploadedTypeIsCheckedAgain(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com", "Ada's Diner")

	upload := requestUpload(t, ada, adaOrg, "menu_item_photo", "image/jpeg")
	send(t, app, http.MethodPut, upload.UploadURL, "application/pdf", pixel).AssertStatus(t, http.StatusOK)

	ada.Post(collection(adaOrg)+"/"+upload.ID+"/confirm", nil).
		AssertProblem(t, http.StatusUnprocessableEntity, "unsupported_image_type")
}
