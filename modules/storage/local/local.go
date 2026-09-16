// Package local stores objects on the local disk: development's storage
// driver (ADR-0075). Objects live under <root>/objects/<key>; each has a
// sidecar under <root>/meta/<key>.json with its content type, ETag and
// metadata. Signed URLs are HMAC-signed links served by [Store.Handler],
// which the app mounts (for example at /storage/).
//
// Stability: experimental (ADR-0054).
package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/modules/storage"
)

// Store is a local-disk [storage.Store].
type Store struct {
	root    string
	secret  []byte
	baseURL string
	now     func() time.Time
}

// Option configures a Store.
type Option func(*Store)

// WithSigner enables signed URLs: secret signs them and baseURL (such as
// http://127.0.0.1:8080/storage) is where [Store.Handler] is mounted.
func WithSigner(secret []byte, baseURL string) Option {
	return func(s *Store) { s.secret, s.baseURL = secret, strings.TrimRight(baseURL, "/") }
}

// New opens the store under root, creating it.
func New(root string, opts ...Option) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: abs, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	for _, dir := range []string{"objects", "meta"} {
		if err := os.MkdirAll(filepath.Join(abs, dir), 0o700); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Root is the store's directory.
func (s *Store) Root() string { return s.root }

// Info implements [storage.Store].
func (s *Store) Info() storage.Info {
	return storage.Info{Driver: "local", Bucket: filepath.Base(s.root), Endpoint: s.root, Local: true}
}

// Ping implements [storage.Store].
func (s *Store) Ping(context.Context) error {
	if _, err := os.Stat(filepath.Join(s.root, "objects")); err != nil {
		return fmt.Errorf("%w: %v", storage.ErrUnavailable, err)
	}
	return nil
}

type meta struct {
	ContentType string            `json:"content_type"`
	ETag        string            `json:"etag"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (s *Store) paths(key string) (obj, mt string, err error) {
	if !storage.ValidKey(key) {
		return "", "", storage.ErrInvalidKey
	}
	return filepath.Join(s.root, "objects", filepath.FromSlash(key)), filepath.Join(s.root, "meta", filepath.FromSlash(key)+".json"), nil
}

// Put implements [storage.Store].
func (s *Store) Put(_ context.Context, key string, r io.Reader, _ int64, opts storage.PutOptions) (storage.Object, error) {
	obj, mt, err := s.paths(key)
	if err != nil {
		return storage.Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(obj), 0o700); err != nil {
		return storage.Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(mt), 0o700); err != nil {
		return storage.Object{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(obj), ".upload-*")
	if err != nil {
		return storage.Object{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return storage.Object{}, err
	}
	if err := os.Rename(tmp.Name(), obj); err != nil {
		_ = os.Remove(tmp.Name())
		return storage.Object{}, err
	}
	m := meta{ContentType: opts.ContentType, ETag: hex.EncodeToString(h.Sum(nil))[:32], Metadata: lower(opts.Metadata)}
	if m.ContentType == "" {
		m.ContentType = storage.ContentTypeFor(key)
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(mt, data, 0o600); err != nil {
		return storage.Object{}, err
	}
	return storage.Object{Key: key, Size: n, ContentType: m.ContentType, ETag: m.ETag, LastModified: s.now().UTC(), Metadata: m.Metadata}, nil
}

func lower(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}

// Stat implements [storage.Store].
func (s *Store) Stat(_ context.Context, key string) (storage.Object, error) {
	obj, mt, err := s.paths(key)
	if err != nil {
		return storage.Object{}, err
	}
	info, err := os.Stat(obj)
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
		return storage.Object{}, storage.ErrNotFound
	} else if err != nil {
		return storage.Object{}, err
	}
	m := meta{ContentType: storage.ContentTypeFor(key)}
	if data, err := os.ReadFile(mt); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return storage.Object{Key: key, Size: info.Size(), ContentType: m.ContentType, ETag: m.ETag, LastModified: info.ModTime().UTC(), Metadata: m.Metadata}, nil
}

// Get implements [storage.Store].
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.Object, error) {
	o, err := s.Stat(ctx, key)
	if err != nil {
		return nil, storage.Object{}, err
	}
	obj, _, _ := s.paths(key)
	f, err := os.Open(obj)
	if err != nil {
		return nil, storage.Object{}, err
	}
	return f, o, nil
}

// Delete implements [storage.Store].
func (s *Store) Delete(_ context.Context, key string) error {
	obj, mt, err := s.paths(key)
	if err != nil {
		return err
	}
	if err := os.Remove(obj); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(mt)
	// Empty directories go too, so listings stay clean.
	for _, p := range []string{filepath.Dir(obj), filepath.Dir(mt)} {
		for p != filepath.Join(s.root, "objects") && p != filepath.Join(s.root, "meta") {
			if os.Remove(p) != nil {
				break
			}
			p = filepath.Dir(p)
		}
	}
	return nil
}

// Copy implements [storage.Store].
func (s *Store) Copy(ctx context.Context, src, dst string) (storage.Object, error) {
	r, o, err := s.Get(ctx, src)
	if err != nil {
		return storage.Object{}, err
	}
	defer r.Close()
	return s.Put(ctx, dst, r, o.Size, storage.PutOptions{ContentType: o.ContentType, Metadata: o.Metadata})
}

// List implements [storage.Store]. The cursor is the last key of the
// previous page.
func (s *Store) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	if !storage.ValidPrefix(opts.Prefix) {
		return storage.Page{}, storage.ErrInvalidKey
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = storage.DefaultListLimit
	}
	limit = min(limit, storage.MaxListLimit)
	base := filepath.Join(s.root, "objects")
	var keys []string
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".upload-") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, opts.Prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return storage.Page{}, err
	}
	sort.Strings(keys)
	page := storage.Page{Objects: []storage.Object{}, Prefixes: []string{}}
	seen := map[string]bool{}
	count := 0
	for _, key := range keys {
		if opts.Cursor != "" && key <= opts.Cursor {
			continue
		}
		if !opts.Recursive {
			rest := strings.TrimPrefix(key, opts.Prefix)
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				dir := opts.Prefix + rest[:i+1]
				if !seen[dir] {
					seen[dir] = true
					page.Prefixes = append(page.Prefixes, dir)
					count++
				}
				if count >= limit {
					page.NextCursor = key
					break
				}
				continue
			}
		}
		if storage.IsDirectoryMarker(key) {
			if opts.Recursive {
				continue
			}
			continue
		}
		o, err := s.Stat(ctx, key)
		if err != nil {
			continue
		}
		page.Objects = append(page.Objects, o)
		count++
		if count >= limit {
			page.NextCursor = key
			break
		}
	}
	// Directories that only hold a marker still appear.
	if !opts.Recursive {
		for _, key := range keys {
			if storage.IsDirectoryMarker(key) {
				rest := strings.TrimPrefix(key, opts.Prefix)
				if i := strings.IndexByte(rest, '/'); i >= 0 {
					dir := opts.Prefix + rest[:i+1]
					if !seen[dir] {
						seen[dir] = true
						page.Prefixes = append(page.Prefixes, dir)
					}
				}
			}
		}
		sort.Strings(page.Prefixes)
	}
	return page, nil
}

// SignedURL implements [storage.Store]: baseURL/<key>?exp=<unix>&method=&sig=.
func (s *Store) SignedURL(_ context.Context, key, method string, expiry time.Duration) (string, error) {
	if s.secret == nil || s.baseURL == "" {
		return "", errors.New("local storage: signed URLs need WithSigner")
	}
	if !storage.ValidKey(key) {
		return "", storage.ErrInvalidKey
	}
	if !storage.ValidSignedMethod(method) {
		return "", fmt.Errorf("storage: signed URLs allow GET or PUT, not %s", method)
	}
	exp := s.now().Add(storage.ClampExpiry(expiry)).Unix()
	q := url.Values{"exp": {strconv.FormatInt(exp, 10)}, "method": {method}}
	q.Set("sig", s.sign(key, method, exp))
	escaped := strings.Join(strings.Split(key, "/"), "/") // keys are already valid
	return s.baseURL + "/" + (&url.URL{Path: escaped}).EscapedPath() + "?" + q.Encode(), nil
}

func (s *Store) sign(key, method string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	fmt.Fprintf(mac, "%s\n%s\n%d", method, key, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// Handler serves signed URLs: GET downloads the object, PUT stores the
// body. Mount it where [WithSigner]'s baseURL points, with the prefix
// stripped (http.StripPrefix).
func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		q := r.URL.Query()
		exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
		if err != nil || q.Get("method") != r.Method || !storage.ValidKey(key) {
			http.Error(w, "invalid signed URL", http.StatusForbidden)
			return
		}
		if s.now().Unix() > exp {
			http.Error(w, "signed URL expired", http.StatusForbidden)
			return
		}
		if !hmac.Equal([]byte(q.Get("sig")), []byte(s.sign(key, r.Method, exp))) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			f, o, err := s.Get(r.Context(), key)
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			} else if err != nil {
				http.Error(w, "storage error", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", o.ContentType)
			w.Header().Set("Content-Length", strconv.FormatInt(o.Size, 10))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", "inline; filename=\""+path.Base(key)+"\"")
			_, _ = io.Copy(w, f)
		case http.MethodPut:
			_, err := s.Put(r.Context(), key, http.MaxBytesReader(w, r.Body, 1<<30), r.ContentLength, storage.PutOptions{ContentType: r.Header.Get("Content-Type")})
			if err != nil {
				http.Error(w, "storage error", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
