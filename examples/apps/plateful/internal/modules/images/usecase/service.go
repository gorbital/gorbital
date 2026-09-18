// Package usecase holds the images module's operations, one file each. They
// are the app's half of an upload that never passes through the app: the
// client asks for a place to put a file, puts it there itself, and comes
// back to say it is done. Everything the app knows about the bytes it
// learns from the store afterwards.
package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/settings"
	"gorbital.dev/modules/storage"

	"example.com/plateful/internal/modules/images/domain"
)

// Permissions the images routes require. Permission names are public API.
//
// Both are organisation permissions: a member holds them through their role
// in the restaurant's organisation (module.go), an API key only when its
// scopes include them. Writing covers requesting an upload, confirming it
// and deleting the image, because all three change what the restaurant's
// page shows.
const (
	PermRead  = "images.image.read"
	PermWrite = "images.image.write"
)

// Audit actions, public API: add new ones, never rename. The confirmation
// is its own event rather than an update, because it is the moment the app
// first learns what was actually uploaded.
const (
	ActionCreated   = "images.image.created"
	ActionConfirmed = "images.image.confirmed"
	ActionDeleted   = "images.image.deleted"
)

// How long a signed URL lives. Both are far below
// storage.MaxSignedURLExpiry, the seven days S3 allows and the store
// enforces; they are short because a signed URL is a bearer token for one
// object, and whoever holds it needs no other credential.
const (
	// UploadExpiry is how long a client has to PUT the bytes. Long enough
	// for a large photo on a slow connection, short enough that a URL found
	// in a log later is useless.
	UploadExpiry = 15 * time.Minute
	// ViewExpiry is how long a signed download URL lives. A page renders in
	// seconds; five minutes leaves room for a slow client and a retry.
	ViewExpiry = 5 * time.Minute
)

// Service runs the images use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	objects  storage.Store
	recorder audit.Recorder
	logger   *slog.Logger
	// maxBytes is the images.max_bytes setting, read when an upload is
	// confirmed rather than when it is requested: operators change the limit
	// in /ops/settings without a deploy, and the limit that matters is the
	// one in force when the file is measured.
	maxBytes *settings.Setting[int]
	now      func() time.Time
	newID    func() string
}

// NewService returns a Service storing rows in store and objects in
// objects. Every dependency may be nil while the OpenAPI document is
// exported, when no use case runs; objects is also nil in an app with no
// file storage configured at all, and then every operation answers
// ErrStorageUnavailable rather than pretending an upload is possible.
func NewService(store Store, objects storage.Store, recorder audit.Recorder, logger *slog.Logger, maxBytes *settings.Setting[int]) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, objects: objects, recorder: recorder, logger: logger, maxBytes: maxBytes, now: time.Now, newID: newID}
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newID returns "img_" and 128 random bits. The randomness matters more
// here than for most IDs: the ID is a segment of the object's key, so a
// guessable ID would be a guessable key.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return "img_" + idEncoding.EncodeToString(b)
}

// clock returns the time at PostgreSQL's microsecond precision, so a stored
// time equals the one returned.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// bucket returns the app's file storage, or ErrStorageUnavailable. The
// framework hands modules a nil gorbital.Deps.Storage when the app has none,
// and this module is nothing without it, so it says so with a 503 rather
// than failing somewhere deeper with a nil pointer.
func (s *Service) bucket() (storage.Store, error) {
	if s.objects == nil {
		return nil, domain.ErrStorageUnavailable
	}
	return s.objects, nil
}

// limit returns the largest image the platform accepts, in bytes.
func (s *Service) limit(ctx context.Context) int64 {
	if s.maxBytes == nil {
		return 0 // no limit declared: only the store's own limits apply
	}
	return int64(s.maxBytes.Get(ctx))
}

// memberID returns the member acting in orgID, the organisation in the
// request's path. guard.OrgMember checked the membership and the permission
// and left the organisation in the actor; a request acting in no
// organisation or in another one, such as on a route without the guard,
// gets ErrUnauthenticated.
func memberID(ctx context.Context, orgID string) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" || orgID == "" || a.OrgID != orgID {
		return "", domain.ErrUnauthenticated
	}
	return a.ID, nil
}

// audit records an event after the change it describes; a failed audit
// write is logged, not returned. The recorder adds the actor, the
// organisation and the request.
func (s *Service) audit(ctx context.Context, action, id string, metadata map[string]any) {
	e := audit.Event{Action: action, ResourceType: "image", ResourceID: id, Outcome: audit.OutcomeSuccess, Metadata: metadata}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record images audit event", "action", action, "err", err)
	}
}

// storeError returns the module's own errors as they are and hides the
// rest, such as driver errors, which aren't API. The storage package's
// errors are translated here too: storage.ErrNotFound means the object
// isn't there, which for this module is an upload that never happened, and
// storage.ErrUnavailable means the bucket can't be reached, which is the
// same answer to a caller as no storage at all.
func storeError(op string, err error) error {
	known := []error{
		domain.ErrInvalidImage, domain.ErrImageNotFound, domain.ErrImageNotUploaded,
		domain.ErrImageTooLarge, domain.ErrUnsupportedImageType, domain.ErrStorageUnavailable,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return domain.ErrImageNotUploaded
	case errors.Is(err, storage.ErrUnavailable):
		return domain.ErrStorageUnavailable
	}
	return fmt.Errorf("images: %s: %v", op, err) //nolint:errorlint // driver errors aren't API
}
