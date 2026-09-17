// Package logarchive keeps an app's log records in its file storage
// (ADR-0079). Every record the logger writes is copied, as a JSON line, to
// a spool file for the current hour under a local directory. At the top of
// the hour the finished file is gzipped, uploaded to the app's
// [storage.Store] as logs/<service>/<YYYY>/<MM>/<DD>/<HH>.jsonl.gz and
// removed. A graceful shutdown, or the setting turned off, uploads the
// partial hour as <HH>.partial-<unix>.jsonl.gz.
//
// Collection is controlled by a live [config.Value], usually a runtime
// setting: off (the default) collects nothing; on collects from the next
// record. The archive never blocks logging: records go to a buffered file,
// and uploads run in their own goroutine. A failed upload is logged and
// retried at the next tick; the file stays on disk until it succeeds, and a
// file left by a previous run is uploaded at the first tick.
//
// Stability: experimental (ADR-0054).
package logarchive

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/modules/storage"
)

const (
	// DefaultDir is where apps spool the current hour before it is
	// uploaded, next to the local storage driver's default.
	DefaultDir = ".orb/logs"
	// DefaultInterval is how often the uploader looks for finished hours,
	// the setting turned off, and files a failed upload left behind.
	DefaultInterval = time.Minute
	// ContentType is the content type of every archived object.
	ContentType = "application/gzip"
	// KeyPrefix starts every archived object's key.
	KeyPrefix = "logs/"

	// stampLayout names a spool file after its hour, in UTC.
	stampLayout = "2006-01-02T15"
	// keyLayout is the hour's part of an object key.
	keyLayout     = "2006/01/02/15"
	spoolSuffix   = ".jsonl"
	gzipSuffix    = ".gz"
	partialMarker = "partial-"
	bufferSize    = 64 << 10
	uploadTimeout = 5 * time.Minute
	// maxInstanceLength bounds the instance name in a key.
	maxInstanceLength = 64
)

// An Option configures [New].
type Option interface{ apply(*Archive) }

type optionFunc func(*Archive)

func (f optionFunc) apply(a *Archive) { f(a) }

// WithLevel sets the lowest level archived. Default: info, so the archive
// matches a logger at the default level whatever the app's own level.
func WithLevel(level slog.Leveler) Option {
	return optionFunc(func(a *Archive) { a.level = level })
}

// WithInstance names this instance in every key, as
// logs/<service>/<YYYY>/<MM>/<DD>/<HH>.<instance>.jsonl.gz, so instances
// sharing a bucket don't overwrite each other's hours. Use the host name or
// the container's name; characters other than letters, digits, '-', '_'
// and '.' are replaced by '-', and it is cut at 64. Default: none.
func WithInstance(name string) Option {
	return optionFunc(func(a *Archive) { a.instance = cleanInstance(name) })
}

// WithInterval sets how often the uploader runs. Default:
// [DefaultInterval].
func WithInterval(d time.Duration) Option {
	return optionFunc(func(a *Archive) { a.interval = d })
}

// WithClock sets the clock, for tests.
func WithClock(now func() time.Time) Option {
	return optionFunc(func(a *Archive) { a.now = now })
}

// Status describes the archive for operators and tests.
type Status struct {
	// Collecting reports whether records are archived right now: the
	// setting is on and there is a store.
	Collecting bool `json:"collecting"`
	// Dir is the spool directory.
	Dir string `json:"dir"`
	// Pending counts the files waiting for an upload, the current hour
	// excluded: finished hours and partial hours whose upload failed.
	Pending int `json:"pending"`
	// LastKey is the key of the last successful upload.
	LastKey      string    `json:"last_key,omitempty"`
	LastUploadAt time.Time `json:"last_upload_at,omitzero"`
	// LastError is the last upload or spool failure since the last
	// success.
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
}

// binding is what Bind sets: where uploads go, what turns collection on and
// who hears about failures.
type binding struct {
	store   storage.Store
	enabled config.Value[bool]
	logger  *slog.Logger
}

// An Archive spools log records by the hour and uploads finished hours. Its
// [Archive.Handler] joins the app's logger, usually as a tee
// (gorbital.dev/modules/telemetry's WithLogTee), and [Archive.Bind]
// connects it to the store and the setting once they exist. It is safe for
// concurrent use.
type Archive struct {
	dir      string
	service  string
	instance string
	level    slog.Leveler
	interval time.Duration
	now      func() time.Time

	binding atomic.Pointer[binding]
	warned  atomic.Bool // "on without a store" was logged

	mu       sync.Mutex
	file     *os.File
	buf      *bufio.Writer
	stamp    string // the open spool's hour, stampLayout
	closed   bool
	spoolErr error // the last failure to open or write the spool, reported by the uploader
	// lastPartial and partialSeq keep partial names unique within a second.
	lastPartial string
	partialSeq  int

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}
	kick      chan struct{}

	statusMu sync.Mutex
	status   Status
}

// New returns an archive spooling under dir (created on demand) for
// service, whose name starts every key. Nothing is collected until
// [Archive.Bind].
func New(dir, service string, opts ...Option) (*Archive, error) {
	a := &Archive{
		dir: dir, service: service, level: slog.LevelInfo, interval: DefaultInterval, now: time.Now,
		stop: make(chan struct{}), done: make(chan struct{}), kick: make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt.apply(a)
	}
	var errs []error
	if dir == "" {
		errs = append(errs, errors.New("a directory is required"))
	}
	if service == "" || !storage.ValidKey(KeyPrefix+service+"/x") || strings.Contains(service, "/") {
		errs = append(errs, fmt.Errorf("service %q must be a valid key segment", service))
	}
	if a.interval <= 0 {
		errs = append(errs, errors.New("the interval must be positive"))
	}
	if a.level == nil {
		errs = append(errs, errors.New("the level must not be nil"))
	}
	if a.now == nil {
		errs = append(errs, errors.New("the clock must not be nil"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("logarchive: invalid archive: %w", err)
	}
	a.status.Dir = dir
	return a, nil
}

// Handler returns the handler that spools records: a JSON line each, with a
// service attribute, at [WithLevel] and above, while collection is on.
func (a *Archive) Handler() slog.Handler {
	inner := slog.NewJSONHandler(spoolWriter{a}, &slog.HandlerOptions{Level: a.level})
	return &handler{a: a, inner: inner.WithAttrs([]slog.Attr{slog.String("service", a.service)})}
}

// Bind connects the archive to the store uploads go to, the setting that
// turns collection on, and the logger that reports uploads and failures,
// and starts the uploader. A nil store collects nothing and logs one
// warning when the setting is on; a nil enabled is off; a nil logger
// discards reports. Bind again to replace them.
func (a *Archive) Bind(store storage.Store, enabled config.Value[bool], logger *slog.Logger) {
	if enabled == nil {
		enabled = config.Static(false)
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	a.binding.Store(&binding{store: store, enabled: enabled, logger: logger})
	a.startOnce.Do(func() { go a.loop() })
}

// Close stops the uploader, uploads the partial hour (and anything else
// waiting) and stops collecting. Upload failures are logged, not returned:
// the files stay for the next start.
func (a *Archive) Close(ctx context.Context) error {
	a.stopOnce.Do(func() {
		// A never-bound archive never started the loop.
		a.startOnce.Do(func() { close(a.done) })
		close(a.stop)
		<-a.done
	})
	a.mu.Lock()
	a.closed = true
	err := a.closeSpoolLocked(true)
	a.mu.Unlock()
	b := a.binding.Load()
	if b == nil {
		return err
	}
	if err != nil {
		b.logger.ErrorContext(ctx, "log archive: closing the spool file", "dir", a.dir, "err", err)
	}
	if b.store != nil {
		a.uploadPending(ctx, b)
	}
	return nil
}

// Status describes the archive.
func (a *Archive) Status() Status {
	a.statusMu.Lock()
	s := a.status
	a.statusMu.Unlock()
	s.Collecting = a.collecting(context.Background())
	for _, name := range a.pending() {
		if !a.isCurrent(name) {
			s.Pending++
		}
	}
	return s
}

// collecting reports whether records are archived right now, warning once
// when the setting is on but there is no store.
func (a *Archive) collecting(ctx context.Context) bool {
	b := a.binding.Load()
	if b == nil || !b.enabled.Get(ctx) {
		return false
	}
	if b.store == nil {
		if a.warned.CompareAndSwap(false, true) {
			b.logger.WarnContext(ctx, "log archive is on but the app has no file storage: nothing is collected")
		}
		return false
	}
	return true
}

// loop runs the uploader until Close.
func (a *Archive) loop() {
	defer close(a.done)
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
		case <-a.kick:
		}
		a.tick(context.Background())
	}
}

// tick closes the spool when its hour is over or the setting went off,
// flushes it otherwise, and uploads every file waiting.
func (a *Archive) tick(ctx context.Context) {
	b := a.binding.Load()
	if b == nil {
		return
	}
	on := b.store != nil && b.enabled.Get(ctx)
	a.mu.Lock()
	var err error
	switch {
	case a.file == nil:
	case !on:
		err = a.closeSpoolLocked(true) // the partial hour
	case a.stamp != a.now().UTC().Format(stampLayout):
		err = a.closeSpoolLocked(false) // the finished hour
	default:
		err = a.buf.Flush()
	}
	spoolErr := errors.Join(a.spoolErr, err)
	a.spoolErr = nil
	a.mu.Unlock()
	if spoolErr != nil {
		a.setError(spoolErr)
		b.logger.ErrorContext(ctx, "log archive can't write its spool file; records since are lost to the archive", "dir", a.dir, "err", spoolErr)
	}
	if b.store != nil {
		a.uploadPending(ctx, b)
	}
}

// wake runs the uploader soon.
func (a *Archive) wake() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// openSpoolLocked opens the hour's file for appending, so a restart within
// the hour continues it.
func (a *Archive) openSpoolLocked(stamp string) error {
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(a.dir, stamp+spoolSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // under the spool directory, named after the hour
	if err != nil {
		return err
	}
	a.file, a.buf, a.stamp = f, bufio.NewWriterSize(f, bufferSize), stamp
	return nil
}

// closeSpoolLocked flushes and closes the open file, if any. A partial hour
// is renamed so the uploader tells it from a finished one; the current
// hour's name is free again for records that follow.
func (a *Archive) closeSpoolLocked(partial bool) error {
	if a.file == nil {
		return nil
	}
	err := errors.Join(a.buf.Flush(), a.file.Close())
	path := filepath.Join(a.dir, a.stamp+spoolSuffix)
	a.file, a.buf, a.stamp = nil, nil, ""
	if partial {
		err = errors.Join(err, os.Rename(path, a.partialName(path)))
	}
	return err
}

// partialName names a partial hour after the second it ended, with a
// counter when that name was used in this second already (on disk, or
// uploaded and removed since), so two partials never share a key.
func (a *Archive) partialName(path string) string {
	base := strings.TrimSuffix(path, spoolSuffix) + "." + partialMarker + strconv.FormatInt(a.now().Unix(), 10)
	if base == a.lastPartial {
		a.partialSeq++
	} else {
		a.lastPartial, a.partialSeq = base, 0
	}
	for {
		to := base + spoolSuffix
		if a.partialSeq > 0 {
			to = base + "-" + strconv.Itoa(a.partialSeq) + spoolSuffix
		}
		if _, err := os.Lstat(to); errors.Is(err, os.ErrNotExist) {
			return to
		}
		a.partialSeq++
	}
}

// isCurrent reports whether name is the open spool file.
func (a *Archive) isCurrent(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file != nil && name == a.stamp+spoolSuffix
}

// pending lists the spool files in the directory, oldest first, and
// removes compressed copies a failed run left without their source.
func (a *Archive) pending() []string {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
		case strings.HasSuffix(name, spoolSuffix):
			names = append(names, name)
		case strings.HasSuffix(name, spoolSuffix+gzipSuffix):
			if _, err := os.Stat(filepath.Join(a.dir, strings.TrimSuffix(name, gzipSuffix))); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(filepath.Join(a.dir, name))
			}
		}
	}
	slices.Sort(names)
	return names
}

// uploadPending uploads every finished or partial hour, oldest first.
func (a *Archive) uploadPending(ctx context.Context, b *binding) {
	for _, name := range a.pending() {
		if a.isCurrent(name) {
			continue
		}
		key, err := a.keyFor(name)
		if err != nil {
			continue // not a spool file of ours
		}
		a.upload(ctx, b, name, key)
	}
}

// upload gzips one spool file next to it, stores the result under key and
// removes both. On failure the spool file stays for the next tick.
func (a *Archive) upload(ctx context.Context, b *binding, name, key string) {
	path := filepath.Join(a.dir, name)
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Size() == 0 {
		_ = os.Remove(path) // an hour with nothing in it
		return
	}
	gz := path + gzipSuffix
	size, err := compress(path, gz)
	if err == nil {
		err = a.put(ctx, b.store, key, gz, size)
	}
	if err != nil {
		_ = os.Remove(gz)
		a.setError(err)
		b.logger.ErrorContext(ctx, "log archive upload failed; retrying at the next tick", "key", key, "bytes", info.Size(), "err", err)
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(gz)
	a.statusMu.Lock()
	a.status.LastKey, a.status.LastUploadAt, a.status.LastError, a.status.LastErrorAt = key, a.now().UTC(), "", time.Time{}
	a.statusMu.Unlock()
	b.logger.InfoContext(ctx, "log archive uploaded", "key", key, "bytes", info.Size(), "compressed_bytes", size)
}

// put stores the compressed file.
func (a *Archive) put(ctx context.Context, store storage.Store, key, gz string, size int64) error {
	f, err := os.Open(gz) //nolint:gosec // the compressed copy this archive just wrote
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	_, err = store.Put(ctx, key, f, size, storage.PutOptions{ContentType: ContentType, Metadata: map[string]string{"service": a.service}})
	return err
}

func (a *Archive) setError(err error) {
	a.statusMu.Lock()
	a.status.LastError, a.status.LastErrorAt = err.Error(), a.now().UTC()
	a.statusMu.Unlock()
}

// keyFor derives an object key from a spool file's name:
// 2026-09-17T10.jsonl becomes logs/<service>/2026/09/17/10.jsonl.gz, and
// 2026-09-17T10.partial-1758105000.jsonl keeps its partial suffix; the
// instance, when set, follows the hour.
func (a *Archive) keyFor(name string) (string, error) {
	stamp, suffix, _ := strings.Cut(strings.TrimSuffix(name, spoolSuffix), ".")
	hour, err := time.Parse(stampLayout, stamp)
	if err != nil {
		return "", fmt.Errorf("logarchive: %q is not a spool file", name)
	}
	if suffix != "" && (!strings.HasPrefix(suffix, partialMarker) || suffix == partialMarker) {
		return "", fmt.Errorf("logarchive: %q is not a spool file", name)
	}
	key := KeyPrefix + a.service + "/" + hour.Format(keyLayout)
	if a.instance != "" {
		key += "." + a.instance
	}
	if suffix != "" {
		key += "." + suffix
	}
	key += spoolSuffix + gzipSuffix
	if !storage.ValidKey(key) {
		return "", fmt.Errorf("logarchive: invalid key %q", key)
	}
	return key, nil
}

// compress gzips src into dst and returns dst's size.
func compress(src, dst string) (int64, error) {
	in, err := os.Open(src) //nolint:gosec // a spool file under the archive's directory
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // next to the spool file, under the archive's directory
	if err != nil {
		return 0, err
	}
	zw := gzip.NewWriter(out)
	zw.Name = filepath.Base(src)
	_, err = io.Copy(zw, in)
	if err := errors.Join(err, zw.Close(), out.Close()); err != nil {
		return 0, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// cleanInstance makes name safe in a key segment.
func cleanInstance(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= maxInstanceLength {
			break
		}
	}
	return strings.Trim(b.String(), ".")
}

// spoolWriter is where the JSON handler writes: each call is one record.
type spoolWriter struct{ a *Archive }

// Write appends p to the current hour's file, opening it or the next
// hour's as needed. It never fails: a file that can't be written is
// reported by the uploader, and the record is dropped from the archive.
func (w spoolWriter) Write(p []byte) (int, error) {
	a := w.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return len(p), nil
	}
	stamp := a.now().UTC().Format(stampLayout)
	if a.file != nil && a.stamp != stamp {
		if err := a.closeSpoolLocked(false); err != nil {
			a.spoolErr = err
		}
		a.wake() // upload the finished hour
	}
	if a.file == nil {
		if err := a.openSpoolLocked(stamp); err != nil {
			a.spoolErr = err
			return len(p), nil
		}
	}
	if _, err := a.buf.Write(p); err != nil {
		a.spoolErr = err
	}
	return len(p), nil
}

// handler spools records while collection is on.
type handler struct {
	a     *Archive
	inner slog.Handler
}

// Enabled reports whether a record at level is archived right now.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level) && h.a.collecting(ctx)
}

// Handle writes r as a JSON line to the spool.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a handler adding attrs to every record.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{a: h.a, inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a handler opening a group.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{a: h.a, inner: h.inner.WithGroup(name)}
}
