package usecase

import (
	"context"
	"net/http"
	"time"

	"example.com/plateful/internal/modules/images/domain"
)

// View is an image and a short-lived signed URL that downloads it.
type View struct {
	Image domain.Image
	// URL downloads the object with a GET, for anyone holding it.
	URL string
	// ExpiresAt is when URL stops working.
	ExpiresAt time.Time
}

// docs:start signed-get-url

// GetImage returns one of the organisation orgID's images with a signed GET
// URL that lasts ViewExpiry.
//
// The row is read by organisation as well as by ID, so the org_id is
// checked again here even though guard.OrgMember already proved membership:
// the guard says which organisation the caller is acting in, and this says
// the image belongs to that one. An image of another organisation is
// ErrImageNotFound, the same answer as one that doesn't exist, and the URL
// is only signed after that check passes. The key itself is never returned
// to the client, so a caller has nothing to substitute into a later
// request.
//
// The URL is signed fresh on every read and dies in five minutes, which is
// how a private bucket serves a public-looking page: the image tag's src is
// a bearer token for one object, so it is made to expire before it can be
// passed around. storage.MaxSignedURLExpiry would allow seven days; a page
// that renders in seconds has no use for that.
//
// A pending image gets a URL too. The object may not exist yet, in which
// case the URL answers 404 — the status in the response is what tells the
// client which it is.
func (s *Service) GetImage(ctx context.Context, orgID, id string) (View, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return View{}, err
	}
	bucket, err := s.bucket()
	if err != nil {
		return View{}, err
	}
	image, err := s.store.SelectImage(ctx, orgID, id, false)
	if err != nil {
		return View{}, storeError("get", err)
	}
	url, err := bucket.SignedURL(ctx, image.StorageKey, http.MethodGet, ViewExpiry)
	if err != nil {
		return View{}, storeError("sign download", err)
	}
	return View{Image: image, URL: url, ExpiresAt: s.clock().Add(ViewExpiry)}, nil
}

// docs:end signed-get-url
