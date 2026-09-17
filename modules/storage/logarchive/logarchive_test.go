package logarchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/modules/storage"
)

// fakeStore records uploads and fails on request.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]fakeObject
	fail    atomic.Bool
	puts    atomic.Int32
}

type fakeObject struct {
	body        []byte
	contentType string
	metadata    map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string]fakeObject{}} }

func (s *fakeStore) Put(_ context.Context, key string, r io.Reader, size int64, opts storage.PutOptions) (storage.Object, error) {
	s.puts.Add(1)
	if s.fail.Load() {
		return storage.Object{}, errors.New("bucket on fire")
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return storage.Object{}, err
	}
	if int64(len(body)) != size {
		return storage.Object{}, errors.New("size mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = fakeObject{body: body, contentType: opts.ContentType, metadata: opts.Metadata}
	return storage.Object{Key: key, Size: size, ContentType: opts.ContentType}, nil
}

func (s *fakeStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// lines gunzips an object into its JSON lines.
func (s *fakeStore) lines(t *testing.T, key string) []string {
	t.Helper()
	s.mu.Lock()
	obj, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		t.Fatalf("no object %q; have %v", key, s.keys())
	}
	if obj.contentType != ContentType {
		t.Errorf("content type = %q, want %q", obj.contentType, ContentType)
	}
	zr, err := gzip.NewReader(bytes.NewReader(obj.body))
	if err != nil {
		t.Fatalf("gunzip %s: %v", key, err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func (s *fakeStore) Info() storage.Info                   { return storage.Info{Driver: "fake"} }
func (s *fakeStore) Ping(context.Context) error           { return nil }
func (s *fakeStore) Delete(context.Context, string) error { return nil }
func (s *fakeStore) Get(context.Context, string) (io.ReadCloser, storage.Object, error) {
	return nil, storage.Object{}, storage.ErrNotFound
}
func (s *fakeStore) Stat(context.Context, string) (storage.Object, error) {
	return storage.Object{}, storage.ErrNotFound
}
func (s *fakeStore) Copy(context.Context, string, string) (storage.Object, error) {
	return storage.Object{}, storage.ErrNotFound
}
func (s *fakeStore) List(context.Context, storage.ListOptions) (storage.Page, error) {
	return storage.Page{}, nil
}
func (s *fakeStore) SignedURL(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

// toggle is a live boolean setting.
type toggle struct{ on atomic.Bool }

func (t *toggle) Get(context.Context) bool { return t.on.Load() }

// clock is a fake clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

var start = time.Date(2026, 9, 17, 10, 30, 0, 0, time.UTC)

// harness is an archive bound to a fake store, with a fake clock, an
// unstarted uploader (tests call tick) and a logger through the archive's
// own handler, as in an app, that also keeps what was logged.
type harness struct {
	a      *Archive
	store  *fakeStore
	on     *toggle
	clock  *clock
	logger *slog.Logger
	logged *bytes.Buffer
	dir    string
}

func newHarness(t *testing.T, opts ...Option) *harness {
	t.Helper()
	h := &harness{store: newFakeStore(), on: &toggle{}, clock: &clock{t: start}, logged: &bytes.Buffer{}, dir: filepath.Join(t.TempDir(), "logs")}
	a, err := New(h.dir, "acme-api", append([]Option{WithClock(h.clock.now)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	h.a = a
	// The app's logger: text to a buffer, teed into the archive.
	primary := slog.NewTextHandler(h.logged, nil)
	h.logger = slog.New(tee{primary: primary, tee: a.Handler()})
	a.binding.Store(&binding{store: h.store, enabled: h.on, logger: h.logger})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return h
}

// tee sends records to both handlers, as telemetry's tee does.
type tee struct{ primary, tee slog.Handler }

func (h tee) Enabled(ctx context.Context, l slog.Level) bool {
	return h.primary.Enabled(ctx, l) || h.tee.Enabled(ctx, l)
}

func (h tee) Handle(ctx context.Context, r slog.Record) error {
	if h.tee.Enabled(ctx, r.Level) {
		_ = h.tee.Handle(ctx, r.Clone())
	}
	return h.primary.Handle(ctx, r)
}

func (h tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	return tee{primary: h.primary.WithAttrs(attrs), tee: h.tee.WithAttrs(attrs)}
}

func (h tee) WithGroup(name string) slog.Handler {
	return tee{primary: h.primary.WithGroup(name), tee: h.tee.WithGroup(name)}
}

func (h *harness) files(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestOffCollectsNothing(t *testing.T) {
	h := newHarness(t)
	h.logger.Info("hello")
	h.a.tick(context.Background())
	if files := h.files(t); files != nil {
		t.Errorf("files = %v, want no directory while off", files)
	}
	if got := h.store.keys(); len(got) != 0 {
		t.Errorf("uploaded %v while off", got)
	}
	if s := h.a.Status(); s.Collecting || s.Pending != 0 || s.Dir != h.dir {
		t.Errorf("status = %+v", s)
	}
	if !strings.Contains(h.logged.String(), "hello") {
		t.Error("the primary handler lost the record")
	}
}

func TestHourlyRotation(t *testing.T) {
	h := newHarness(t)
	h.on.on.Store(true)
	ctx := context.Background()

	h.logger.With("component", "api").InfoContext(ctx, "first", "n", 1)
	h.logger.WithGroup("req").WarnContext(ctx, "second", "id", "r1")
	h.logger.Debug("not archived at info")
	if files := h.files(t); !slices.Equal(files, []string{"2026-09-17T10.jsonl"}) {
		t.Fatalf("files = %v", files)
	}
	if s := h.a.Status(); !s.Collecting || s.Pending != 0 {
		t.Errorf("status = %+v", s)
	}

	// Nothing to upload within the hour; the buffer is flushed to disk.
	h.a.tick(ctx)
	if got := h.store.keys(); len(got) != 0 {
		t.Fatalf("uploaded %v within the hour", got)
	}
	if raw, _ := os.ReadFile(filepath.Join(h.dir, "2026-09-17T10.jsonl")); strings.Count(string(raw), "\n") != 2 {
		t.Errorf("spool after a tick = %q, want two lines", raw)
	}

	// The next hour's first record closes the file; the tick uploads it.
	h.clock.set(start.Add(time.Hour))
	h.logger.InfoContext(ctx, "third")
	select {
	case <-h.a.kick:
	default:
		t.Error("the finished hour didn't wake the uploader")
	}
	h.a.tick(ctx)
	want := "logs/acme-api/2026/09/17/10.jsonl.gz"
	if got := h.store.keys(); !slices.Equal(got, []string{want}) {
		t.Fatalf("keys = %v, want %v", got, []string{want})
	}
	lines := h.store.lines(t, want)
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	for i, wants := range [][]string{
		{`"msg":"first"`, `"component":"api"`, `"n":1`, `"service":"acme-api"`, `"level":"INFO"`},
		{`"msg":"second"`, `"req":{"id":"r1"}`, `"level":"WARN"`},
	} {
		for _, w := range wants {
			if !strings.Contains(lines[i], w) {
				t.Errorf("line %d = %s, want %s", i, lines[i], w)
			}
		}
	}
	if files := h.files(t); !slices.Equal(files, []string{"2026-09-17T11.jsonl"}) {
		t.Errorf("files after upload = %v, want the new hour only", files)
	}
	if s := h.a.Status(); s.LastKey != want || s.LastUploadAt.IsZero() || s.LastError != "" || s.Pending != 0 {
		t.Errorf("status = %+v", s)
	}
	if !strings.Contains(h.logged.String(), "log archive uploaded") || !strings.Contains(h.logged.String(), want) {
		t.Errorf("logged = %q, want the upload reported", h.logged.String())
	}

	// A quiet hour: the tick closes the file without a record.
	h.clock.set(start.Add(2 * time.Hour))
	h.a.tick(ctx)
	if got := h.store.keys(); len(got) != 2 || got[1] != "logs/acme-api/2026/09/17/11.jsonl.gz" {
		t.Errorf("keys = %v, want the 11 o'clock hour too", got)
	}
	// The upload's own log record opened the new hour's file.
	if files := h.files(t); !slices.Equal(files, []string{"2026-09-17T12.jsonl"}) {
		t.Errorf("files = %v, want the current hour only", files)
	}
}

func TestCloseUploadsThePartialHour(t *testing.T) {
	h := newHarness(t)
	h.on.on.Store(true)
	ctx := context.Background()
	h.logger.InfoContext(ctx, "before shutdown")
	h.clock.set(start.Add(5 * time.Minute))
	if err := h.a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	want := "logs/acme-api/2026/09/17/10.partial-" + itoa(start.Add(5*time.Minute).Unix()) + ".jsonl.gz"
	if got := h.store.keys(); !slices.Equal(got, []string{want}) {
		t.Fatalf("keys = %v, want %v", got, []string{want})
	}
	if lines := h.store.lines(t, want); len(lines) != 1 || !strings.Contains(lines[0], "before shutdown") {
		t.Errorf("lines = %q", lines)
	}
	if files := h.files(t); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
	// Records after Close aren't collected, and Close again is fine.
	h.logger.InfoContext(ctx, "after shutdown")
	if err := h.a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if files := h.files(t); len(files) != 0 {
		t.Errorf("files after close = %v", files)
	}
}

func TestTogglingOff(t *testing.T) {
	h := newHarness(t)
	h.on.on.Store(true)
	ctx := context.Background()
	h.logger.InfoContext(ctx, "while on")

	h.on.on.Store(false)
	h.logger.InfoContext(ctx, "while off")
	h.a.tick(ctx)
	want := "logs/acme-api/2026/09/17/10.partial-" + itoa(start.Unix()) + ".jsonl.gz"
	if got := h.store.keys(); !slices.Equal(got, []string{want}) {
		t.Fatalf("keys = %v, want %v", got, []string{want})
	}
	if lines := h.store.lines(t, want); len(lines) != 1 || !strings.Contains(lines[0], "while on") {
		t.Errorf("lines = %q, want the record from before the switch only", lines)
	}
	if files := h.files(t); len(files) != 0 {
		t.Errorf("files = %v, want none while off", files)
	}

	// On again in the same hour: a new file for the rest of the hour, and a
	// second partial at the same second gets its own name.
	h.on.on.Store(true)
	h.logger.InfoContext(ctx, "on again")
	h.on.on.Store(false)
	h.a.tick(ctx)
	h.on.on.Store(true)
	h.logger.InfoContext(ctx, "on a third time")
	h.on.on.Store(false)
	h.a.tick(ctx)
	got := h.store.keys()
	wantKeys := []string{want, "logs/acme-api/2026/09/17/10.partial-" + itoa(start.Unix()) + "-1.jsonl.gz", "logs/acme-api/2026/09/17/10.partial-" + itoa(start.Unix()) + "-2.jsonl.gz"}
	slices.Sort(wantKeys)
	if !slices.Equal(got, wantKeys) {
		t.Errorf("keys = %v, want %v", got, wantKeys)
	}
}

func TestUploadFailureIsRetried(t *testing.T) {
	h := newHarness(t)
	h.on.on.Store(true)
	ctx := context.Background()
	h.logger.InfoContext(ctx, "kept")
	h.clock.set(start.Add(time.Hour))
	h.store.fail.Store(true)
	h.a.tick(ctx)
	if got := h.store.keys(); len(got) != 0 {
		t.Fatalf("keys = %v, want none after a failure", got)
	}
	// The failure's own log record opened the current hour's file.
	if files := h.files(t); !slices.Equal(files, []string{"2026-09-17T10.jsonl", "2026-09-17T11.jsonl"}) {
		t.Errorf("files = %v, want the spool kept (and no .gz left)", files)
	}
	if s := h.a.Status(); !strings.Contains(s.LastError, "bucket on fire") || s.LastErrorAt.IsZero() || s.Pending != 1 {
		t.Errorf("status = %+v", s)
	}
	if !strings.Contains(h.logged.String(), "log archive upload failed") {
		t.Errorf("logged = %q, want the failure", h.logged.String())
	}

	h.store.fail.Store(false)
	h.a.tick(ctx)
	want := "logs/acme-api/2026/09/17/10.jsonl.gz"
	if got := h.store.keys(); !slices.Equal(got, []string{want}) {
		t.Fatalf("keys = %v, want %v after the retry", got, []string{want})
	}
	if lines := h.store.lines(t, want); len(lines) != 1 || !strings.Contains(lines[0], "kept") {
		t.Errorf("lines = %q", lines)
	}
	if s := h.a.Status(); s.LastError != "" || s.Pending != 0 || s.LastKey != want {
		t.Errorf("status = %+v", s)
	}
}

func TestLeftoversFromAPreviousRun(t *testing.T) {
	h := newHarness(t)
	h.on.on.Store(true)
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"2026-09-16T23.jsonl":                    "{\"msg\":\"yesterday\"}\n",
		"2026-09-17T09.partial-1758100000.jsonl": "{\"msg\":\"crashed\"}\n",
		"2026-09-17T08.jsonl":                    "", // an empty hour: dropped
		"notes.txt":                              "not ours",
		"2026-09-17T07.jsonl.gz":                 "orphan of a failed run",
	} {
		if err := os.WriteFile(filepath.Join(h.dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.a.tick(context.Background())
	want := []string{"logs/acme-api/2026/09/16/23.jsonl.gz", "logs/acme-api/2026/09/17/09.partial-1758100000.jsonl.gz"}
	if got := h.store.keys(); !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	// The uploads' log records opened the current hour's file.
	if files := h.files(t); !slices.Equal(files, []string{"2026-09-17T10.jsonl", "notes.txt"}) {
		t.Errorf("files = %v, want the current hour and the stranger", files)
	}
}

func TestNoStoreWarnsOnce(t *testing.T) {
	h := newHarness(t)
	h.a.binding.Store(&binding{store: nil, enabled: h.on, logger: h.logger})
	h.on.on.Store(true)
	ctx := context.Background()
	h.logger.InfoContext(ctx, "one")
	h.logger.InfoContext(ctx, "two")
	h.a.tick(ctx)
	if n := strings.Count(h.logged.String(), "no file storage"); n != 1 {
		t.Errorf("warned %d times: %q", n, h.logged.String())
	}
	if files := h.files(t); files != nil {
		t.Errorf("files = %v, want nothing collected without a store", files)
	}
	if s := h.a.Status(); s.Collecting {
		t.Errorf("status = %+v", s)
	}
}

func TestBindStartsAndCloseFlushes(t *testing.T) {
	// The real loop, at a short interval: the finished hour is uploaded
	// without anyone calling tick.
	dir := filepath.Join(t.TempDir(), "logs")
	c := &clock{t: start}
	a, err := New(dir, "acme-api", WithClock(c.now), WithInterval(10*time.Millisecond), WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	store, on := newFakeStore(), &toggle{}
	on.on.Store(true)
	a.Bind(store, on, slog.New(slog.DiscardHandler))
	logger := slog.New(a.Handler())
	logger.Debug("debug is archived at this level")
	c.set(start.Add(time.Hour))
	deadline := time.Now().Add(5 * time.Second)
	for len(store.keys()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.keys(); !slices.Equal(got, []string{"logs/acme-api/2026/09/17/10.jsonl.gz"}) {
		t.Fatalf("keys = %v", got)
	}
	logger.Info("the partial hour")
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.keys(); len(got) != 2 || !strings.HasPrefix(got[1], "logs/acme-api/2026/09/17/11.partial-") {
		t.Errorf("keys after close = %v", got)
	}
	// Closing an archive that was never bound is fine too.
	unbound, err := New(dir, "acme-api")
	if err != nil {
		t.Fatal(err)
	}
	if err := unbound.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestKeys(t *testing.T) {
	plain, err := New("x", "acme-api")
	if err != nil {
		t.Fatal(err)
	}
	named, err := New("x", "acme-api", WithInstance("web-1.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		a    *Archive
		name string
		want string
	}{
		{plain, "2026-09-17T10.jsonl", "logs/acme-api/2026/09/17/10.jsonl.gz"},
		{plain, "2026-09-17T10.partial-1758105000.jsonl", "logs/acme-api/2026/09/17/10.partial-1758105000.jsonl.gz"},
		{plain, "2026-09-17T10.partial-1758105000-1.jsonl", "logs/acme-api/2026/09/17/10.partial-1758105000-1.jsonl.gz"},
		{named, "2026-09-17T10.jsonl", "logs/acme-api/2026/09/17/10.web-1.example.com.jsonl.gz"},
		{named, "2026-09-17T10.partial-1758105000.jsonl", "logs/acme-api/2026/09/17/10.web-1.example.com.partial-1758105000.jsonl.gz"},
		{plain, "notes.jsonl", ""},
		{plain, "2026-09-17T10.other.jsonl", ""},
		{plain, "2026-09-17T10.partial-.jsonl", ""},
	} {
		got, err := tc.a.keyFor(tc.name)
		if tc.want == "" {
			if err == nil {
				t.Errorf("keyFor(%q) = %q, want an error", tc.name, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("keyFor(%q) = %q, %v; want %q", tc.name, got, err, tc.want)
		}
		if !storage.ValidKey(got) {
			t.Errorf("%q is not a valid key", got)
		}
	}
	if got := cleanInstance("pod/one two.local"); got != "pod-one-two.local" {
		t.Errorf("cleanInstance = %q", got)
	}
	if got := cleanInstance(strings.Repeat("a", 100)); len(got) != maxInstanceLength {
		t.Errorf("cleanInstance kept %d characters", len(got))
	}
}

func TestNewValidation(t *testing.T) {
	for name, opts := range map[string]struct {
		dir, service string
		opts         []Option
	}{
		"no dir":     {"", "svc", nil},
		"no service": {"d", "", nil},
		"slash":      {"d", "a/b", nil},
		"dot":        {"d", "..", nil},
		"interval":   {"d", "svc", []Option{WithInterval(0)}},
		"nil level":  {"d", "svc", []Option{WithLevel(nil)}},
		"nil clock":  {"d", "svc", []Option{WithClock(nil)}},
	} {
		if _, err := New(opts.dir, opts.service, opts.opts...); err == nil {
			t.Errorf("%s: New succeeded", name)
		}
	}
}

// TestContentTypeIsGzip guards the constant the storage screen relies on to
// offer the download.
func TestContentTypeIsGzip(t *testing.T) {
	if ContentType != "application/gzip" || http.DetectContentType([]byte{0x1f, 0x8b, 8}) != "application/x-gzip" {
		t.Errorf("ContentType = %q", ContentType)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
