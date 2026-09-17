// Package storage stores files for an app (ADR-0075): a [Store] is a
// bucket of objects addressed by key, with drivers for the local disk
// (development) and S3-compatible services (Amazon S3, DigitalOcean
// Spaces, Cloudflare R2, MinIO). The Dev Portal's Storage screen and the
// operators' API (/ops/storage) work on any Store.
//
// Stability: experimental (ADR-0054).
package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Errors a Store returns.
var (
	// ErrNotFound reports a key with no object.
	ErrNotFound = errors.New("storage: object not found")
	// ErrInvalidKey reports a key [ValidKey] refuses.
	ErrInvalidKey = errors.New("storage: invalid key")
	// ErrUnavailable reports a driver that can't reach its service.
	ErrUnavailable = errors.New("storage: unavailable")
)

// Limits.
const (
	// MaxKeyLength bounds a key, as S3 does.
	MaxKeyLength = 1024
	// MaxListLimit bounds a List page.
	MaxListLimit = 1000
	// DefaultListLimit is a List page without a limit.
	DefaultListLimit = 200
	// MaxSignedURLExpiry bounds a signed URL's life (7 days, as S3 does).
	MaxSignedURLExpiry = 7 * 24 * time.Hour
)

// Object describes a stored object.
type Object struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// PutOptions describe an upload.
type PutOptions struct {
	// ContentType of the object; guessed from the key when empty.
	ContentType string
	// Metadata is kept with the object (S3 user metadata; a sidecar file
	// on disk). Keys are lowercased; values must be plain text.
	Metadata map[string]string
}

// ListOptions select a page of objects.
type ListOptions struct {
	// Prefix keeps keys starting with it.
	Prefix string
	// Recursive lists every key under the prefix; otherwise keys with a
	// slash after the prefix are folded into Prefixes (directories).
	Recursive bool
	// Cursor continues a previous page.
	Cursor string
	// Limit is the page size (default [DefaultListLimit], at most
	// [MaxListLimit]).
	Limit int
}

// Page is one page of a listing.
type Page struct {
	Objects []Object `json:"objects"`
	// Prefixes are the "directories" folded when not recursive.
	Prefixes []string `json:"prefixes"`
	// NextCursor continues the listing; empty at the end.
	NextCursor string `json:"next_cursor,omitempty"`
}

// Info describes a store for operators and the portal.
type Info struct {
	// Driver is local, s3, spaces, r2 or minio.
	Driver string `json:"driver"`
	Bucket string `json:"bucket"`
	// Endpoint is the service's address (the directory for local).
	Endpoint string `json:"endpoint,omitempty"`
	Region   string `json:"region,omitempty"`
	// Local reports a store on this machine, safe to change freely.
	Local bool `json:"local"`
	// PublicURL is where objects are reachable without a signature, when
	// the bucket is public; empty otherwise.
	PublicURL string `json:"public_url,omitempty"`
}

// A Store is a bucket of objects. Implementations are safe for concurrent
// use.
type Store interface {
	// Info describes the store.
	Info() Info
	// Ping checks the service answers; [ErrUnavailable] wraps the reason.
	Ping(ctx context.Context) error
	// Put stores r under key, replacing any object there.
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (Object, error)
	// Get opens the object for reading; the caller closes it.
	Get(ctx context.Context, key string) (io.ReadCloser, Object, error)
	// Stat describes the object without reading it.
	Stat(ctx context.Context, key string) (Object, error)
	// Delete removes the object; a missing key isn't an error.
	Delete(ctx context.Context, key string) error
	// Copy duplicates src to dst.
	Copy(ctx context.Context, src, dst string) (Object, error)
	// List returns a page of objects under a prefix.
	List(ctx context.Context, opts ListOptions) (Page, error)
	// SignedURL returns a URL that lets its holder GET (download) or PUT
	// (upload) the key until expiry, without other credentials.
	SignedURL(ctx context.Context, key string, method string, expiry time.Duration) (string, error)
}

// Move copies src to dst and deletes src. It is a helper for every Store,
// since object stores have no rename.
func Move(ctx context.Context, s Store, src, dst string) (Object, error) {
	obj, err := s.Copy(ctx, src, dst)
	if err != nil {
		return Object{}, err
	}
	if err := s.Delete(ctx, src); err != nil {
		return Object{}, err
	}
	return obj, nil
}

// ValidKey reports whether key can name an object: 1 to [MaxKeyLength]
// bytes of valid UTF-8, no leading slash, no empty or "." or ".."
// segment, no control characters.
func ValidKey(key string) bool {
	if key == "" || len(key) > MaxKeyLength || !utf8.ValidString(key) || strings.HasPrefix(key, "/") {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ValidPrefix reports whether prefix can select objects: empty, or a
// valid key optionally ending with a slash.
func ValidPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	return ValidKey(strings.TrimSuffix(prefix, "/"))
}

// DirectoryMarker is the key of the object that makes an empty
// "directory" exist: prefix + "/.keep". Listings hide it.
func DirectoryMarker(prefix string) string {
	return strings.TrimSuffix(prefix, "/") + "/.keep"
}

// IsDirectoryMarker reports a key [DirectoryMarker] made.
func IsDirectoryMarker(key string) bool { return strings.HasSuffix(key, "/.keep") }

// ValidSignedMethod reports a method [Store.SignedURL] accepts.
func ValidSignedMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodPut
}

// ClampExpiry bounds a signed URL's expiry: at least a second, at most
// [MaxSignedURLExpiry]; zero means an hour.
func ClampExpiry(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return time.Hour
	case d < time.Second:
		return time.Second
	case d > MaxSignedURLExpiry:
		return MaxSignedURLExpiry
	}
	return d
}

// ContentTypeFor guesses a content type from a key's extension, falling
// back to application/octet-stream.
func ContentTypeFor(key string) string {
	ext := ""
	if i := strings.LastIndexByte(key, '.'); i >= 0 && i > strings.LastIndexByte(key, '/') {
		ext = strings.ToLower(key[i:])
	}
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

var contentTypes = map[string]string{
	".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8", ".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".json": "application/json", ".xml": "application/xml", ".csv": "text/csv; charset=utf-8",
	".pdf": "application/pdf", ".zip": "application/zip", ".gz": "application/gzip", ".tar": "application/x-tar",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml", ".ico": "image/x-icon", ".avif": "image/avif",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf",
}
