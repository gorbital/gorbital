// Package s3 stores objects in an S3-compatible service: Amazon S3,
// DigitalOcean Spaces, Cloudflare R2 or MinIO (ADR-0075), through the
// MinIO client. Signed URLs are the service's presigned URLs.
//
// Stability: experimental (ADR-0054).
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"gorbital.dev/config"
	"gorbital.dev/modules/storage"
)

// Config connects to a service.
type Config struct {
	// Driver names the service for operators: s3, spaces, r2 or minio.
	Driver string
	// Endpoint is the service's host, without a scheme: s3.amazonaws.com,
	// nyc3.digitaloceanspaces.com, <account>.r2.cloudflarestorage.com,
	// 127.0.0.1:9000 for MinIO.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey config.Secret
	// UseSSL is on for every service but a local MinIO.
	UseSSL bool
	// PathStyle addresses the bucket in the path (MinIO) rather than the
	// host.
	PathStyle bool
	// PublicURL is where the bucket's objects are reachable when public.
	PublicURL string
}

// Store is an S3-compatible [storage.Store].
type Store struct {
	cfg    Config
	client *minio.Client
}

// New connects to the service; it doesn't check the bucket (Ping does).
func New(cfg Config) (*Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("s3 storage: endpoint and bucket are required")
	}
	if cfg.AccessKey == "" || cfg.SecretKey.Reveal() == "" {
		return nil, errors.New("s3 storage: access key and secret key are required")
	}
	if cfg.Driver == "" {
		cfg.Driver = "s3"
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey.Reveal(), ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 storage: %w", err)
	}
	return &Store{cfg: cfg, client: client}, nil
}

// Info implements [storage.Store].
func (s *Store) Info() storage.Info {
	return storage.Info{Driver: s.cfg.Driver, Bucket: s.cfg.Bucket, Endpoint: s.cfg.Endpoint, Region: s.cfg.Region, Local: false, PublicURL: s.cfg.PublicURL}
}

// Ping implements [storage.Store]: the bucket must exist.
func (s *Store) Ping(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.cfg.Bucket)
	if err != nil {
		return fmt.Errorf("%w: %v", storage.ErrUnavailable, err)
	}
	if !ok {
		return fmt.Errorf("%w: bucket %q doesn't exist", storage.ErrUnavailable, s.cfg.Bucket)
	}
	return nil
}

func (s *Store) checkKey(key string) error {
	if !storage.ValidKey(key) {
		return storage.ErrInvalidKey
	}
	return nil
}

func object(key string, info minio.ObjectInfo) storage.Object {
	return storage.Object{Key: key, Size: info.Size, ContentType: info.ContentType, ETag: strings.Trim(info.ETag, `"`), LastModified: info.LastModified.UTC(), Metadata: userMetadata(info.UserMetadata)}
}

func userMetadata(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(strings.TrimPrefix(k, "X-Amz-Meta-"))] = v
	}
	return out
}

// Put implements [storage.Store]. size may be -1 when unknown.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, opts storage.PutOptions) (storage.Object, error) {
	if err := s.checkKey(key); err != nil {
		return storage.Object{}, err
	}
	ct := opts.ContentType
	if ct == "" {
		ct = storage.ContentTypeFor(key)
	}
	info, err := s.client.PutObject(ctx, s.cfg.Bucket, key, r, size, minio.PutObjectOptions{ContentType: ct, UserMetadata: opts.Metadata})
	if err != nil {
		return storage.Object{}, wrap(err)
	}
	return storage.Object{Key: key, Size: info.Size, ContentType: ct, ETag: strings.Trim(info.ETag, `"`), LastModified: info.LastModified.UTC(), Metadata: opts.Metadata}, nil
}

// Stat implements [storage.Store].
func (s *Store) Stat(ctx context.Context, key string) (storage.Object, error) {
	if err := s.checkKey(key); err != nil {
		return storage.Object{}, err
	}
	info, err := s.client.StatObject(ctx, s.cfg.Bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return storage.Object{}, wrap(err)
	}
	return object(key, info), nil
}

// Get implements [storage.Store].
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.Object, error) {
	if err := s.checkKey(key); err != nil {
		return nil, storage.Object{}, err
	}
	obj, err := s.client.GetObject(ctx, s.cfg.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, storage.Object{}, wrap(err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, storage.Object{}, wrap(err)
	}
	return obj, object(key, info), nil
}

// Delete implements [storage.Store].
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.checkKey(key); err != nil {
		return err
	}
	err := s.client.RemoveObject(ctx, s.cfg.Bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !errors.Is(wrap(err), storage.ErrNotFound) {
		return wrap(err)
	}
	return nil
}

// Copy implements [storage.Store].
func (s *Store) Copy(ctx context.Context, src, dst string) (storage.Object, error) {
	if err := s.checkKey(src); err != nil {
		return storage.Object{}, err
	}
	if err := s.checkKey(dst); err != nil {
		return storage.Object{}, err
	}
	_, err := s.client.CopyObject(ctx, minio.CopyDestOptions{Bucket: s.cfg.Bucket, Object: dst}, minio.CopySrcOptions{Bucket: s.cfg.Bucket, Object: src})
	if err != nil {
		return storage.Object{}, wrap(err)
	}
	return s.Stat(ctx, dst)
}

// List implements [storage.Store]. The cursor is the last key of the
// previous page (S3's StartAfter).
func (s *Store) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	if !storage.ValidPrefix(opts.Prefix) {
		return storage.Page{}, storage.ErrInvalidKey
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = storage.DefaultListLimit
	}
	limit = min(limit, storage.MaxListLimit)
	page := storage.Page{Objects: []storage.Object{}, Prefixes: []string{}}
	count := 0
	for info := range s.client.ListObjects(ctx, s.cfg.Bucket, minio.ListObjectsOptions{Prefix: opts.Prefix, Recursive: opts.Recursive, StartAfter: opts.Cursor, MaxKeys: limit + 1}) {
		if info.Err != nil {
			return storage.Page{}, wrap(info.Err)
		}
		if count >= limit {
			page.NextCursor = lastKey(page)
			break
		}
		if strings.HasSuffix(info.Key, "/") {
			page.Prefixes = append(page.Prefixes, info.Key)
			count++
			continue
		}
		if storage.IsDirectoryMarker(info.Key) {
			if !opts.Recursive {
				continue
			}
			continue
		}
		page.Objects = append(page.Objects, object(info.Key, info))
		count++
	}
	return page, nil
}

func lastKey(p storage.Page) string {
	var last string
	if n := len(p.Objects); n > 0 {
		last = p.Objects[n-1].Key
	}
	if n := len(p.Prefixes); n > 0 && p.Prefixes[n-1] > last {
		last = p.Prefixes[n-1]
	}
	return last
}

// SignedURL implements [storage.Store] with a presigned URL.
func (s *Store) SignedURL(ctx context.Context, key, method string, expiry time.Duration) (string, error) {
	if err := s.checkKey(key); err != nil {
		return "", err
	}
	if !storage.ValidSignedMethod(method) {
		return "", fmt.Errorf("storage: signed URLs allow GET or PUT, not %s", method)
	}
	expiry = storage.ClampExpiry(expiry)
	var u *url.URL
	var err error
	if method == http.MethodPut {
		u, err = s.client.PresignedPutObject(ctx, s.cfg.Bucket, key, expiry)
	} else {
		u, err = s.client.PresignedGetObject(ctx, s.cfg.Bucket, key, expiry, nil)
	}
	if err != nil {
		return "", wrap(err)
	}
	return u.String(), nil
}

// wrap maps the client's errors to the package's.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "NoSuchKey", "NotFound":
		return storage.ErrNotFound
	case "NoSuchBucket", "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return fmt.Errorf("%w: %s", storage.ErrUnavailable, resp.Message)
	}
	if resp.StatusCode == http.StatusNotFound {
		return storage.ErrNotFound
	}
	return err
}
