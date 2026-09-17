package portal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// LogStore keeps the app's log records on disk under .orb/portal/logs, so
// they survive the app's restarts and orb dev's (ADR-0072). Records come
// from the app's output (JSON log lines are parsed; other lines are kept
// as they are), from orb dev's own messages and, when services run, from
// the PostgreSQL container. The store is a set of JSON Lines segments,
// rotated at [SegmentBytes] and bounded to [MaxStoreBytes] in total, the
// oldest segment dropped first. Queries scan the segments newest first.
// It is safe for concurrent use.
type LogStore struct {
	dir  string
	mu   sync.Mutex
	seq  int64 // the last record ID
	file *os.File
	size int64 // of the open segment
	subs map[*logSubscription]struct{}
	now  func() time.Time
	// Limits, set by NewLogStore.
	segmentBytes, maxBytes int64
}

// Limits of the store.
const (
	// SegmentBytes is when a segment file is rotated.
	SegmentBytes = 8 << 20
	// MaxStoreBytes is the store's size, the oldest segment dropped past it.
	MaxStoreBytes = 64 << 20
	// MaxLogQuery is the most records one query returns.
	MaxLogQuery = 1000
	// logsDir is where the segments live, inside the app.
	logsDir = portalDir + "/logs"
	// logFiltersFile keeps the saved filters.
	logFiltersFile = portalDir + "/log-filters.json"
	// maxLogLineBytes cuts longer lines.
	maxLogLineBytes = 16 << 10
)

// Log sources.
const (
	SourceApp      = "app"
	SourceHTTP     = "http"
	SourceAuth     = "auth"
	SourceJobs     = "jobs"
	SourceMail     = "mail"
	SourceStorage  = "storage"
	SourcePostgres = "postgres"
	SourceOrb      = "orb"
)

// LogRecord is one record as the store keeps it.
type LogRecord struct {
	// ID orders records; it is the cursor.
	ID   int64     `json:"id"`
	Time time.Time `json:"time"`
	// Source is where the record came from: http, auth, jobs, mail,
	// storage, postgres, orb, or app for the rest.
	Source string `json:"source"`
	// Level is DEBUG, INFO, WARN or ERROR.
	Level   string `json:"level"`
	Message string `json:"message"`
	// Attrs are the record's attributes in order, groups flattened into
	// dotted keys, values as text. A raw line (not a log record) has none.
	Attrs []LogAttr `json:"attrs,omitempty"`
	// Raw is the line as written when it wasn't a log record.
	Raw string `json:"raw,omitempty"`
}

// LogAttr is one attribute.
type LogAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Attr returns the value of key, or "".
func (r LogRecord) Attr(key string) string {
	for _, a := range r.Attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

// NewLogStore opens the store under the app directory dir, creating it.
func NewLogStore(dir string) (*LogStore, error) {
	s := &LogStore{dir: dir, subs: map[*logSubscription]struct{}{}, now: time.Now, segmentBytes: SegmentBytes, maxBytes: MaxStoreBytes}
	if err := os.MkdirAll(filepath.Join(dir, logsDir), 0o700); err != nil {
		return nil, err
	}
	segs, err := s.segments()
	if err != nil {
		return nil, err
	}
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		s.seq, err = lastID(filepath.Join(dir, logsDir, last.name))
		if err != nil {
			return nil, err
		}
		// Keep appending to the last segment while it has room.
		if last.size < s.segmentBytes {
			f, err := os.OpenFile(filepath.Join(dir, logsDir, last.name), os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				return nil, err
			}
			s.file, s.size = f, last.size
		}
	}
	return s, nil
}

// Close closes the open segment.
func (s *LogStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// segment is one file: logs-<first ID>.jsonl.
type segment struct {
	name  string
	first int64
	size  int64
}

// segments lists the files, oldest first.
func (s *LogStore) segments() ([]segment, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, logsDir))
	if err != nil {
		return nil, err
	}
	var segs []segment
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), "logs-")
		rest, ok2 := strings.CutSuffix(rest, ".jsonl")
		if e.IsDir() || !ok || !ok2 {
			continue
		}
		first, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		segs = append(segs, segment{name: e.Name(), first: first, size: info.Size()})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].first < segs[j].first })
	return segs, nil
}

// lastID returns the ID of the last record in a segment file.
func lastID(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var last int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes+1024)
	for sc.Scan() {
		var r struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			last = r.ID
		}
	}
	return last, sc.Err()
}

// Add stores one record, filling its ID and, when zero, its time.
func (s *LogStore) Add(r LogRecord) (LogRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	r.ID = s.seq
	if r.Time.IsZero() {
		r.Time = s.now()
	}
	r.Time = r.Time.UTC()
	if r.Level == "" {
		r.Level = "INFO"
	}
	if r.Source == "" {
		r.Source = SourceApp
	}
	line, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if s.file == nil || s.size+int64(len(line))+1 > s.segmentBytes {
		if err := s.rotate(r.ID); err != nil {
			return r, err
		}
	}
	if _, err := s.file.Write(append(line, '\n')); err != nil {
		return r, err
	}
	s.size += int64(len(line)) + 1
	for sub := range s.subs {
		select {
		case sub.c <- r:
		default:
			sub.dropped++
		}
	}
	return r, nil
}

// rotate opens a new segment starting at ID and drops the oldest ones
// past the size limit.
func (s *LogStore) rotate(id int64) error {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	segs, err := s.segments()
	if err != nil {
		return err
	}
	var total int64
	for _, seg := range segs {
		total += seg.size
	}
	for len(segs) > 0 && total > s.maxBytes-s.segmentBytes {
		if err := os.Remove(filepath.Join(s.dir, logsDir, segs[0].name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		total -= segs[0].size
		segs = segs[1:]
	}
	f, err := os.OpenFile(filepath.Join(s.dir, logsDir, fmt.Sprintf("logs-%012d.jsonl", id)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.file, s.size = f, 0
	return nil
}

// Clear deletes every record.
func (s *LogStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	segs, err := s.segments()
	if err != nil {
		return err
	}
	for _, seg := range segs {
		if err := os.Remove(filepath.Join(s.dir, logsDir, seg.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// LogStats describes the store.
type LogStats struct {
	// Bytes on disk and the limits.
	Bytes    int64 `json:"bytes"`
	MaxBytes int64 `json:"max_bytes"`
	Segments int   `json:"segments"`
	// Oldest is the time of the oldest kept record, when there is one.
	Oldest *time.Time `json:"oldest,omitempty"`
	// Records is how many are kept.
	Records int64  `json:"records"`
	Dir     string `json:"dir"`
}

// Stats reports the store's size.
func (s *LogStore) Stats() (LogStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	segs, err := s.segments()
	if err != nil {
		return LogStats{}, err
	}
	st := LogStats{MaxBytes: s.maxBytes, Segments: len(segs), Dir: logsDir}
	for _, seg := range segs {
		st.Bytes += seg.size
	}
	if len(segs) > 0 {
		err := s.scanFile(filepath.Join(s.dir, logsDir, segs[0].name), func(r LogRecord) bool {
			if st.Oldest == nil {
				t := r.Time
				st.Oldest = &t
			}
			return true
		})
		if err != nil {
			return LogStats{}, err
		}
		first, err := s.firstID(segs)
		if err != nil {
			return LogStats{}, err
		}
		if first > 0 {
			st.Records = s.seq - first + 1
		}
	}
	return st, nil
}

// firstID is the ID of the oldest kept record.
func (s *LogStore) firstID(segs []segment) (int64, error) {
	var first int64
	err := s.scanFile(filepath.Join(s.dir, logsDir, segs[0].name), func(r LogRecord) bool {
		first = r.ID
		return false
	})
	return first, err
}

// scanFile calls fn for each record in a segment, oldest first, until fn
// returns false.
func (s *LogStore) scanFile(path string, fn func(LogRecord) bool) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes+1024)
	for sc.Scan() {
		var r LogRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // a cut line at the end of a segment
		}
		if !fn(r) {
			return nil
		}
	}
	return sc.Err()
}

// LogQuery selects records. Zero values don't filter.
type LogQuery struct {
	From, To time.Time
	// Levels keeps records at these levels (DEBUG, INFO, WARN, ERROR);
	// MinLevel keeps records at or above one.
	Levels   []string
	MinLevel string
	Sources  []string
	// User matches the user_id attribute, or the email attribute, exactly.
	User string
	// Method and Path match request records; Path is a prefix.
	Method, Path string
	// StatusClass is "2xx", "3xx", "4xx" or "5xx"; Status one code.
	StatusClass string
	Status      int
	// MinDurationMS keeps request records at least this slow.
	MinDurationMS float64
	// RequestID and TraceID match the attributes exactly.
	RequestID, TraceID string
	// Text is a case-insensitive substring of the message, an attribute
	// value or the raw line.
	Text string
	// Before returns records with an ID below it (paging backwards); After
	// returns records with an ID above it (tailing).
	Before, After int64
	Limit         int
}

// Matches reports whether r satisfies q's filters (not its paging).
func (q LogQuery) Matches(r LogRecord) bool {
	if !q.From.IsZero() && r.Time.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && !r.Time.Before(q.To) {
		return false
	}
	if len(q.Levels) > 0 && !containsFold(q.Levels, r.Level) {
		return false
	}
	if q.MinLevel != "" && levelRank(r.Level) < levelRank(q.MinLevel) {
		return false
	}
	if len(q.Sources) > 0 && !containsFold(q.Sources, r.Source) {
		return false
	}
	if q.User != "" && !strings.EqualFold(r.Attr("user_id"), q.User) && !strings.EqualFold(r.Attr("email"), q.User) && !strings.EqualFold(r.Attr("actor_id"), q.User) {
		return false
	}
	if q.Method != "" && !strings.EqualFold(r.Attr("method"), q.Method) {
		return false
	}
	if q.Path != "" && !strings.HasPrefix(r.Attr("path"), q.Path) && !strings.HasPrefix(r.Attr("route"), q.Path) {
		return false
	}
	if q.StatusClass != "" || q.Status != 0 {
		status, err := strconv.Atoi(r.Attr("status"))
		if err != nil {
			return false
		}
		if q.Status != 0 && status != q.Status {
			return false
		}
		if q.StatusClass != "" && strconv.Itoa(status/100)+"xx" != q.StatusClass {
			return false
		}
	}
	if q.MinDurationMS > 0 {
		d, err := strconv.ParseFloat(r.Attr("duration_ms"), 64)
		if err != nil || d < q.MinDurationMS {
			return false
		}
	}
	if q.RequestID != "" && r.Attr("request_id") != q.RequestID {
		return false
	}
	if q.TraceID != "" && r.Attr("trace_id") != q.TraceID {
		return false
	}
	if q.Text != "" {
		needle := strings.ToLower(q.Text)
		found := strings.Contains(strings.ToLower(r.Message), needle) || strings.Contains(strings.ToLower(r.Raw), needle)
		for i := 0; !found && i < len(r.Attrs); i++ {
			found = strings.Contains(strings.ToLower(r.Attrs[i].Value), needle) || strings.Contains(strings.ToLower(r.Attrs[i].Key), needle)
		}
		if !found {
			return false
		}
	}
	return true
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// levelRank orders levels; unknown levels rank as INFO.
func levelRank(level string) int {
	switch strings.ToUpper(level) {
	case "DEBUG", "TRACE":
		return 0
	case "WARN", "WARNING":
		return 2
	case "ERROR", "FATAL", "PANIC":
		return 3
	}
	return 1
}

// LogPage is a query's answer: records newest first and, when more are
// kept, the cursor for the next page.
type LogPage struct {
	Logs []LogRecord `json:"logs"`
	// NextBefore is the ID to pass as Before for older records; 0 when
	// there are none.
	NextBefore int64 `json:"next_before,omitempty"`
}

// Query returns the newest records matching q, newest first, up to
// q.Limit (default 200, at most [MaxLogQuery]).
func (s *LogStore) Query(q LogQuery) (LogPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, MaxLogQuery)
	s.mu.Lock()
	segs, err := s.segments()
	s.mu.Unlock()
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{Logs: []LogRecord{}}
	// Segments are read newest first; within one, records are collected
	// then reversed.
	for i := len(segs) - 1; i >= 0 && len(page.Logs) < limit; i-- {
		if q.Before != 0 && segs[i].first >= q.Before {
			continue
		}
		var batch []LogRecord
		err := s.scanFile(filepath.Join(s.dir, logsDir, segs[i].name), func(r LogRecord) bool {
			if q.Before != 0 && r.ID >= q.Before {
				return false
			}
			if q.After != 0 && r.ID <= q.After {
				return true
			}
			if !q.To.IsZero() && !r.Time.Before(q.To) {
				return false
			}
			if q.Matches(r) {
				batch = append(batch, r)
			}
			return true
		})
		if err != nil {
			return LogPage{}, err
		}
		for j := len(batch) - 1; j >= 0 && len(page.Logs) < limit; j-- {
			page.Logs = append(page.Logs, batch[j])
		}
		if len(page.Logs) >= limit && (j0(batch, limit, len(page.Logs)) || i > 0) {
			page.NextBefore = page.Logs[len(page.Logs)-1].ID
		}
	}
	return page, nil
}

// j0 reports whether a batch had records left after filling the page.
func j0(batch []LogRecord, limit, have int) bool { return len(batch) > 0 && have >= limit }

// LogBucket counts records in one interval.
type LogBucket struct {
	Time   time.Time `json:"time"`
	Total  int       `json:"total"`
	Levels struct {
		Debug int `json:"debug"`
		Info  int `json:"info"`
		Warn  int `json:"warn"`
		Error int `json:"error"`
	} `json:"levels"`
}

// Histogram counts the records matching q per bucket of width between
// q.From and q.To (the last hour when unset), oldest first, one bucket per
// interval even when empty.
func (s *LogStore) Histogram(q LogQuery, width time.Duration) ([]LogBucket, error) {
	if width <= 0 {
		width = time.Minute
	}
	if q.To.IsZero() {
		q.To = s.now().UTC()
	}
	if q.From.IsZero() {
		q.From = q.To.Add(-time.Hour)
	}
	q.From, q.To = q.From.UTC().Truncate(width), q.To.UTC()
	n := int(q.To.Sub(q.From)/width) + 1
	if n > 10000 {
		return nil, errors.New("too many buckets; widen the interval")
	}
	buckets := make([]LogBucket, n)
	for i := range buckets {
		buckets[i].Time = q.From.Add(time.Duration(i) * width)
	}
	q.Before, q.After, q.Limit = 0, 0, 0
	err := s.scan(q, func(r LogRecord) bool {
		i := int(r.Time.Sub(q.From) / width)
		if i < 0 || i >= n {
			return true
		}
		b := &buckets[i]
		b.Total++
		switch levelRank(r.Level) {
		case 0:
			b.Levels.Debug++
		case 2:
			b.Levels.Warn++
		case 3:
			b.Levels.Error++
		default:
			b.Levels.Info++
		}
		return true
	})
	return buckets, err
}

// scan calls fn with every record matching q, oldest first, skipping
// segments entirely outside the time range where the IDs allow.
func (s *LogStore) scan(q LogQuery, fn func(LogRecord) bool) error {
	s.mu.Lock()
	segs, err := s.segments()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	for _, seg := range segs {
		stop := false
		err := s.scanFile(filepath.Join(s.dir, logsDir, seg.name), func(r LogRecord) bool {
			if !q.To.IsZero() && !r.Time.Before(q.To) {
				stop = true
				return false
			}
			if q.Matches(r) && !fn(r) {
				stop = true
				return false
			}
			return true
		})
		if err != nil || stop {
			return err
		}
	}
	return nil
}

// ErrorGroup is the records sharing a fingerprint: the message's shape
// (numbers, IDs and quoted values replaced) and the top of the error or
// stack attribute.
type ErrorGroup struct {
	Fingerprint string    `json:"fingerprint"`
	Shape       string    `json:"shape"`
	Top         string    `json:"top,omitempty"`
	Source      string    `json:"source"`
	Level       string    `json:"level"`
	Count       int       `json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	// Last is the most recent record, with its request_id when it has one.
	Last LogRecord `json:"last"`
}

// Errors groups the records at WARN and above matching q by fingerprint,
// most recent first.
func (s *LogStore) Errors(q LogQuery) ([]ErrorGroup, error) {
	if q.MinLevel == "" {
		q.MinLevel = "WARN"
	}
	q.Before, q.After, q.Limit = 0, 0, 0
	groups := map[string]*ErrorGroup{}
	err := s.scan(q, func(r LogRecord) bool {
		shape, top := Fingerprint(r)
		key := shape + "\x00" + top
		g, ok := groups[key]
		if !ok {
			g = &ErrorGroup{Fingerprint: fingerprintID(key), Shape: shape, Top: top, Source: r.Source, Level: r.Level, FirstSeen: r.Time}
			groups[key] = g
		}
		g.Count++
		g.LastSeen, g.Last = r.Time, r
		if levelRank(r.Level) > levelRank(g.Level) {
			g.Level = r.Level
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	out := make([]ErrorGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

var (
	fpUUID   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	fpHex    = regexp.MustCompile(`\b[0-9a-fA-F]{12,}\b`)
	fpID     = regexp.MustCompile(`\b[a-z]{2,5}_[A-Za-z0-9]{6,}\b`)
	fpNumber = regexp.MustCompile(`\b\d+(\.\d+)?\b`)
	fpQuoted = regexp.MustCompile(`"[^"]*"|'[^']*'`)
)

// Fingerprint returns the message's shape and the top of the record's
// error or stack: what groups records of the same problem.
func Fingerprint(r LogRecord) (shape, top string) {
	msg := r.Message
	if msg == "" {
		msg = r.Raw
	}
	shape = normalizeShape(msg)
	if stack := r.Attr("stack"); stack != "" {
		top = firstLine(stack)
	} else if e := r.Attr("error"); e != "" {
		top = normalizeShape(firstLine(e))
	} else if e := r.Attr("err"); e != "" {
		top = normalizeShape(firstLine(e))
	}
	return shape, top
}

func normalizeShape(s string) string {
	s = fpQuoted.ReplaceAllString(s, `"…"`)
	s = fpUUID.ReplaceAllString(s, "<uuid>")
	s = fpID.ReplaceAllString(s, "<id>")
	s = fpHex.ReplaceAllString(s, "<hex>")
	s = fpNumber.ReplaceAllString(s, "<n>")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// fingerprintID is a short stable ID for a fingerprint key.
func fingerprintID(key string) string {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	return strconv.FormatUint(h, 36)
}

// logSubscription receives records as they are added.
type logSubscription struct {
	c       chan LogRecord
	dropped int
}

// Subscribe returns a channel of new records; stop with unsubscribe.
func (s *LogStore) Subscribe() (<-chan LogRecord, func()) {
	sub := &logSubscription{c: make(chan LogRecord, subscriberBuffer)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	return sub.c, func() {
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
	}
}

// SavedFilter is a named LogQuery the developer keeps.
type SavedFilter struct {
	Name  string          `json:"name"`
	Query json.RawMessage `json:"query"`
	Saved time.Time       `json:"saved"`
}

var filterNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,59}$`)

// Filters lists the saved filters by name.
func (s *LogStore) Filters() ([]SavedFilter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readFilters()
}

func (s *LogStore) readFilters() ([]SavedFilter, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, logFiltersFile))
	if errors.Is(err, os.ErrNotExist) {
		return []SavedFilter{}, nil
	} else if err != nil {
		return nil, err
	}
	var filters []SavedFilter
	if err := json.Unmarshal(data, &filters); err != nil {
		return nil, fmt.Errorf("%s: %w", logFiltersFile, err)
	}
	for i := range filters { // the file is indented; queries come back compact
		var compact bytes.Buffer
		if json.Compact(&compact, filters[i].Query) == nil {
			filters[i].Query = json.RawMessage(compact.Bytes())
		}
	}
	sort.Slice(filters, func(i, j int) bool { return filters[i].Name < filters[j].Name })
	return filters, nil
}

func (s *LogStore) writeFilters(filters []SavedFilter) error {
	data, err := json.MarshalIndent(filters, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, logFiltersFile), data, 0o600)
}

// SaveFilter adds or replaces a saved filter.
func (s *LogStore) SaveFilter(f SavedFilter) error {
	if !filterNamePattern.MatchString(f.Name) {
		return errors.New("a filter name is 1 to 60 letters, digits, spaces, dots, hyphens or underscores")
	}
	if len(f.Query) == 0 || !json.Valid(f.Query) {
		return errors.New("the filter's query must be a JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, f.Query); err != nil {
		return err
	}
	f.Query = json.RawMessage(compact.Bytes())
	s.mu.Lock()
	defer s.mu.Unlock()
	filters, err := s.readFilters()
	if err != nil {
		return err
	}
	f.Saved = s.now().UTC()
	replaced := false
	for i := range filters {
		if filters[i].Name == f.Name {
			filters[i], replaced = f, true
		}
	}
	if !replaced {
		filters = append(filters, f)
	}
	return s.writeFilters(filters)
}

// DeleteFilter removes a saved filter; a missing one is fine.
func (s *LogStore) DeleteFilter(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filters, err := s.readFilters()
	if err != nil {
		return err
	}
	kept := filters[:0]
	for _, f := range filters {
		if f.Name != name {
			kept = append(kept, f)
		}
	}
	return s.writeFilters(kept)
}

// Ingest turns a line of output into a record and stores it. JSON log
// lines (slog's JSON handler: time, level, msg and attributes) and text
// log lines (slog's text handler: time=… level=… msg=… key=value) are
// parsed; other lines are kept raw. stream is "app", "orb" or "postgres".
func (s *LogStore) Ingest(stream, line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}
	if len(line) > maxLogLineBytes {
		line = line[:maxLogLineBytes] + "…"
	}
	var r LogRecord
	switch {
	case stream == SourcePostgres:
		r = parsePostgresLine(line)
	case strings.HasPrefix(line, "{"):
		var ok bool
		if r, ok = parseJSONLog(line); !ok {
			r = LogRecord{Raw: line}
		}
	case strings.HasPrefix(line, "time="):
		var ok bool
		if r, ok = parseTextLog(line); !ok {
			r = LogRecord{Raw: line}
		}
	default:
		r = LogRecord{Raw: line}
	}
	if r.Source == "" {
		switch {
		case stream == SourceOrb:
			r.Source = SourceOrb
		case r.Raw != "":
			r.Source = SourceApp
		default:
			r.Source = sourceOf(r)
		}
	}
	_, _ = s.Add(r)
}

// sourceOf derives a parsed record's source from its attributes: the
// source attribute when the logger set one, else the record's shape.
func sourceOf(r LogRecord) string {
	if src := r.Attr("source"); src != "" {
		return src
	}
	switch {
	case r.Message == "http request" || (r.Attr("method") != "" && r.Attr("status") != ""):
		return SourceHTTP
	case r.Attr("job") != "" || r.Attr("job_id") != "" || r.Attr("queue") != "" || strings.HasPrefix(r.Message, "producer:") || strings.Contains(r.Message, "River"):
		return SourceJobs
	case r.Attr("message_id") != "" || strings.HasPrefix(r.Message, "mail"):
		return SourceMail
	case strings.HasPrefix(r.Message, "auth") || r.Attr("session_id") != "" || r.Attr("passkey_id") != "":
		return SourceAuth
	case r.Attr("bucket") != "" || strings.HasPrefix(r.Message, "storage"):
		return SourceStorage
	}
	return SourceApp
}

// parseJSONLog reads a slog JSON line.
func parseJSONLog(line string) (LogRecord, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var obj map[string]any
	if dec.Decode(&obj) != nil {
		return LogRecord{}, false
	}
	var r LogRecord
	ok := false
	if t, has := obj["time"].(string); has {
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			r.Time, ok = ts, true
		}
	}
	if lvl, has := obj["level"].(string); has {
		r.Level = strings.ToUpper(lvl)
	}
	if msg, has := obj["msg"].(string); has {
		r.Message, ok = msg, true
	}
	if !ok {
		return LogRecord{}, false
	}
	delete(obj, "time")
	delete(obj, "level")
	delete(obj, "msg")
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		flattenJSON(k, obj[k], &r.Attrs)
	}
	return r, true
}

func flattenJSON(key string, v any, out *[]LogAttr) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			flattenJSON(key+"."+k, t[k], out)
		}
	case nil:
		*out = append(*out, LogAttr{Key: key, Value: ""})
	case string:
		*out = append(*out, LogAttr{Key: key, Value: t})
	case json.Number:
		*out = append(*out, LogAttr{Key: key, Value: t.String()})
	case bool:
		*out = append(*out, LogAttr{Key: key, Value: strconv.FormatBool(t)})
	default:
		b, _ := json.Marshal(t)
		*out = append(*out, LogAttr{Key: key, Value: string(b)})
	}
}

// parseTextLog reads a slog text line: key=value pairs, values quoted
// with Go syntax when they need it.
func parseTextLog(line string) (LogRecord, bool) {
	var r LogRecord
	rest := line
	seen := false
	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			break
		}
		key := rest[:eq]
		if strings.ContainsFunc(key, unicode.IsSpace) {
			break
		}
		rest = rest[eq+1:]
		var value string
		if strings.HasPrefix(rest, `"`) {
			end := 1
			for end < len(rest) {
				if rest[end] == '\\' {
					end += 2
					continue
				}
				if rest[end] == '"' {
					break
				}
				end++
			}
			if end >= len(rest) {
				return LogRecord{}, false
			}
			unq, err := strconv.Unquote(rest[:end+1])
			if err != nil {
				return LogRecord{}, false
			}
			value, rest = unq, rest[end+1:]
		} else {
			sp := strings.IndexByte(rest, ' ')
			if sp < 0 {
				value, rest = rest, ""
			} else {
				value, rest = rest[:sp], rest[sp:]
			}
		}
		switch key {
		case "time":
			ts, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return LogRecord{}, false
			}
			r.Time, seen = ts, true
		case "level":
			r.Level = strings.ToUpper(value)
		case "msg":
			r.Message = value
		default:
			r.Attrs = append(r.Attrs, LogAttr{Key: key, Value: value})
		}
	}
	return r, seen
}

// parsePostgresLine reads a PostgreSQL server log line ("2026-09-16
// 19:00:00.123 UTC [42] LOG:  message"); other lines are raw.
func parsePostgresLine(line string) LogRecord {
	r := LogRecord{Source: SourcePostgres}
	rest := line
	if len(rest) > 23 {
		if ts, err := time.Parse("2006-01-02 15:04:05.000 MST", rest[:27]); err == nil {
			r.Time = ts
			rest = strings.TrimSpace(rest[27:])
		} else if ts, err := time.Parse("2006-01-02 15:04:05.000 MST", rest[:23]); err == nil {
			r.Time = ts
			rest = strings.TrimSpace(rest[23:])
		}
	}
	if strings.HasPrefix(rest, "[") {
		if i := strings.IndexByte(rest, ']'); i > 0 {
			r.Attrs = append(r.Attrs, LogAttr{Key: "pid", Value: rest[1:i]})
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	level, msg, found := strings.Cut(rest, ":")
	if !found || strings.ContainsAny(level, " \t") {
		r.Raw = line
		return r
	}
	switch level {
	case "DEBUG", "LOG", "INFO", "NOTICE", "STATEMENT", "DETAIL", "HINT", "CONTEXT":
		r.Level = "INFO"
		if level == "DEBUG" {
			r.Level = "DEBUG"
		}
	case "WARNING":
		r.Level = "WARN"
	case "ERROR", "FATAL", "PANIC":
		r.Level = "ERROR"
	default:
		r.Raw = line
		return r
	}
	r.Attrs = append(r.Attrs, LogAttr{Key: "severity", Value: level})
	r.Message = strings.TrimSpace(msg)
	return r
}

// Follow ingests every output line the hub publishes until stop is closed.
func (s *LogStore) Follow(hub *Hub, stop <-chan struct{}) {
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)
	for {
		select {
		case <-stop:
			return
		case e := <-sub.C:
			if e.Type == "output" && e.Output != nil {
				s.Ingest(e.Output.Stream, e.Output.Text)
			}
		}
	}
}

// IngestReader ingests each line read from r as stream, until EOF.
func (s *LogStore) IngestReader(stream string, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLogLineBytes+1024)
	for sc.Scan() {
		s.Ingest(stream, sc.Text())
	}
	return sc.Err()
}
