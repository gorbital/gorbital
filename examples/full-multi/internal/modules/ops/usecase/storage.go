package usecase

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"gorbital.dev/audit"
	"gorbital.dev/modules/storage"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// ErrStorageOff reports an app without a store.
var ErrStorageOff = errors.New("storage isn't configured")

// StorageStatus is GET /ops/storage.
type StorageStatus struct {
	storage.Info
	// Status is ok or error, and Error the reason.
	Status string
	Error  string
	// PingDuration is how long the check took.
	PingDuration time.Duration
}

// StorageStatus describes the store and whether it answers.
func (s *Service) StorageStatus(ctx context.Context) (StorageStatus, error) {
	if err := authorize(ctx, opsdomain.PermStorageRead); err != nil {
		return StorageStatus{}, err
	}
	if s.storage == nil {
		return StorageStatus{}, ErrStorageOff
	}
	st := StorageStatus{Info: s.storage.Info(), Status: "ok"}
	start := time.Now()
	if err := s.storage.Ping(ctx); err != nil {
		st.Status, st.Error = "error", err.Error()
	}
	st.PingDuration = time.Since(start)
	return st, nil
}

// ListObjects lists a page of objects under a prefix.
func (s *Service) ListObjects(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	if err := authorize(ctx, opsdomain.PermStorageRead); err != nil {
		return storage.Page{}, err
	}
	if s.storage == nil {
		return storage.Page{}, ErrStorageOff
	}
	return s.storage.List(ctx, opts)
}

// StatObject describes one object.
func (s *Service) StatObject(ctx context.Context, key string) (storage.Object, error) {
	if err := authorize(ctx, opsdomain.PermStorageRead); err != nil {
		return storage.Object{}, err
	}
	if s.storage == nil {
		return storage.Object{}, ErrStorageOff
	}
	return s.storage.Stat(ctx, key)
}

// OpenObject opens an object for download; the caller closes it.
func (s *Service) OpenObject(ctx context.Context, key string) (io.ReadCloser, storage.Object, error) {
	if err := authorize(ctx, opsdomain.PermStorageRead); err != nil {
		return nil, storage.Object{}, err
	}
	if s.storage == nil {
		return nil, storage.Object{}, ErrStorageOff
	}
	return s.storage.Get(ctx, key)
}

// PutObject stores an upload under key.
func (s *Service) PutObject(ctx context.Context, key string, r io.Reader, size int64, opts storage.PutOptions) (storage.Object, error) {
	if err := authorize(ctx, opsdomain.PermStorageWrite); err != nil {
		return storage.Object{}, err
	}
	if s.storage == nil {
		return storage.Object{}, ErrStorageOff
	}
	obj, err := s.storage.Put(ctx, key, r, size, opts)
	if err != nil {
		return storage.Object{}, err
	}
	return obj, s.audit.Record(ctx, audit.Event{Action: "storage.object.uploaded", ResourceType: "storage_object", ResourceID: key,
		Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"size": obj.Size, "content_type": obj.ContentType}})
}

// DeleteObject removes an object.
func (s *Service) DeleteObject(ctx context.Context, key string) error {
	if err := authorize(ctx, opsdomain.PermStorageWrite); err != nil {
		return err
	}
	if s.storage == nil {
		return ErrStorageOff
	}
	if !storage.ValidKey(key) {
		return storage.ErrInvalidKey
	}
	if err := s.storage.Delete(ctx, key); err != nil {
		return err
	}
	return s.audit.Record(ctx, audit.Event{Action: "storage.object.deleted", ResourceType: "storage_object", ResourceID: key, Outcome: audit.OutcomeSuccess})
}

// MoveObject renames an object.
func (s *Service) MoveObject(ctx context.Context, from, to string) (storage.Object, error) {
	if err := authorize(ctx, opsdomain.PermStorageWrite); err != nil {
		return storage.Object{}, err
	}
	if s.storage == nil {
		return storage.Object{}, ErrStorageOff
	}
	if !storage.ValidKey(from) || !storage.ValidKey(to) {
		return storage.Object{}, storage.ErrInvalidKey
	}
	obj, err := storage.Move(ctx, s.storage, from, to)
	if err != nil {
		return storage.Object{}, err
	}
	return obj, s.audit.Record(ctx, audit.Event{Action: "storage.object.moved", ResourceType: "storage_object", ResourceID: to,
		Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"from": from}})
}

// MakeDirectory makes an empty "directory" listable by storing its
// marker object (ADR-0075).
func (s *Service) MakeDirectory(ctx context.Context, prefix string) (string, error) {
	if err := authorize(ctx, opsdomain.PermStorageWrite); err != nil {
		return "", err
	}
	if s.storage == nil {
		return "", ErrStorageOff
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if !storage.ValidKey(prefix) {
		return "", storage.ErrInvalidKey
	}
	if _, err := s.storage.Put(ctx, storage.DirectoryMarker(prefix), strings.NewReader(""), 0, storage.PutOptions{ContentType: "application/x-directory"}); err != nil {
		return "", err
	}
	return prefix + "/", s.audit.Record(ctx, audit.Event{Action: "storage.directory.created", ResourceType: "storage_prefix", ResourceID: prefix + "/", Outcome: audit.OutcomeSuccess})
}

// SignedURL returns a URL that downloads (GET) or uploads (PUT) the key
// until expiry.
func (s *Service) SignedURL(ctx context.Context, key, method string, expiry time.Duration) (string, time.Time, error) {
	if err := authorize(ctx, opsdomain.PermStorageWrite); err != nil {
		return "", time.Time{}, err
	}
	if s.storage == nil {
		return "", time.Time{}, ErrStorageOff
	}
	if !storage.ValidSignedMethod(method) {
		return "", time.Time{}, storage.ErrInvalidKey
	}
	expiry = storage.ClampExpiry(expiry)
	u, err := s.storage.SignedURL(ctx, key, method, expiry)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(expiry).UTC()
	return u, expires, s.audit.Record(ctx, audit.Event{Action: "storage.signed_url.created", ResourceType: "storage_object", ResourceID: key,
		Outcome: audit.OutcomeSuccess, Metadata: map[string]any{"method": method, "expires_at": expires}})
}
