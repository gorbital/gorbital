package portal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLogStore(t *testing.T) *LogStore {
	t.Helper()
	s, err := NewLogStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLogStoreIngestsJSONTextAndRawLines(t *testing.T) {
	s := newTestLogStore(t)
	s.Ingest("app", `{"time":"2026-09-16T10:00:00.5Z","level":"INFO","msg":"http request","service":"x","method":"GET","path":"/v1/me","status":200,"duration_ms":12,"user_id":"usr_1","request_id":"req_a"}`)
	s.Ingest("app", `time=2026-09-16T10:00:01Z level=WARN msg="job ran" service=x job=heartbeat job_id=7`)
	s.Ingest("app", "plain output line")
	s.Ingest("orb", "orb: applying migrations")
	s.Ingest("postgres", `2026-09-16 10:00:02.123 UTC [42] ERROR:  relation "nope" does not exist`)
	s.Ingest("app", "")

	page, err := s.Query(LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Logs) != 5 || page.NextBefore != 0 {
		t.Fatalf("logs = %d, next %d", len(page.Logs), page.NextBefore)
	}
	pg, orb, raw, text, http := page.Logs[0], page.Logs[1], page.Logs[2], page.Logs[3], page.Logs[4]
	if http.Source != SourceHTTP || http.Level != "INFO" || http.Attr("status") != "200" || http.Attr("user_id") != "usr_1" || !http.Time.Equal(time.Date(2026, 9, 16, 10, 0, 0, 500000000, time.UTC)) {
		t.Errorf("http = %+v", http)
	}
	if text.Source != SourceJobs || text.Level != "WARN" || text.Message != "job ran" || text.Attr("job_id") != "7" {
		t.Errorf("text = %+v", text)
	}
	if raw.Source != SourceApp || raw.Raw != "plain output line" || raw.Level != "INFO" {
		t.Errorf("raw = %+v", raw)
	}
	if orb.Source != SourceOrb || orb.Raw != "orb: applying migrations" {
		t.Errorf("orb = %+v", orb)
	}
	if pg.Source != SourcePostgres || pg.Level != "ERROR" || pg.Attr("pid") != "42" || !strings.HasPrefix(pg.Message, "relation") {
		t.Errorf("postgres = %+v", pg)
	}
	if http.ID >= text.ID || text.ID >= raw.ID {
		t.Errorf("IDs not increasing: %d %d %d", http.ID, text.ID, raw.ID)
	}
}

func TestLogStoreFiltersPagesAndSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLogStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for i := range 30 {
		level, status := "INFO", "200"
		if i%10 == 9 {
			level, status = "ERROR", "500"
		}
		_, err := s.Add(LogRecord{Time: base.Add(time.Duration(i) * time.Second), Source: SourceHTTP, Level: level, Message: "http request",
			Attrs: []LogAttr{{"method", "GET"}, {"path", "/v1/items/" + string(rune('a'+i%3))}, {"status", status}, {"duration_ms", "15"}, {"user_id", "usr_" + string(rune('a'+i%2))}, {"request_id", "req_" + string(rune('a'+i))}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Close()
	s, err = NewLogStore(dir) // reopened: IDs continue, records are kept
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, _ := s.Add(LogRecord{Message: "after reopen", Level: "DEBUG"})
	if r.ID != 31 {
		t.Errorf("ID after reopen = %d, want 31", r.ID)
	}

	cases := []struct {
		name string
		q    LogQuery
		want int
	}{
		{"all", LogQuery{Limit: 100}, 31},
		{"errors", LogQuery{Levels: []string{"error"}}, 3},
		{"min warn", LogQuery{MinLevel: "WARN"}, 3},
		{"status class", LogQuery{StatusClass: "5xx"}, 3},
		{"status", LogQuery{Status: 200}, 27},
		{"user", LogQuery{User: "usr_a"}, 15},
		{"path prefix", LogQuery{Path: "/v1/items/a"}, 10},
		{"method", LogQuery{Method: "get"}, 30},
		{"duration", LogQuery{MinDurationMS: 20}, 0},
		{"request", LogQuery{RequestID: "req_b"}, 1},
		{"text", LogQuery{Text: "REOPEN"}, 1},
		{"time", LogQuery{From: base.Add(10 * time.Second), To: base.Add(20 * time.Second)}, 10},
		{"source", LogQuery{Sources: []string{"jobs"}}, 0},
	}
	for _, c := range cases {
		page, err := s.Query(c.q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Logs) != c.want {
			t.Errorf("%s: %d records, want %d", c.name, len(page.Logs), c.want)
		}
	}
	// Paging backwards by ID.
	page, _ := s.Query(LogQuery{Limit: 10})
	if len(page.Logs) != 10 || page.Logs[0].ID != 31 || page.NextBefore != page.Logs[9].ID {
		t.Fatalf("page 1 = %d records, first %d, next %d", len(page.Logs), page.Logs[0].ID, page.NextBefore)
	}
	page2, _ := s.Query(LogQuery{Limit: 10, Before: page.NextBefore})
	if len(page2.Logs) != 10 || page2.Logs[0].ID != page.NextBefore-1 {
		t.Errorf("page 2 = %d records, first %d", len(page2.Logs), page2.Logs[0].ID)
	}
	// Tailing forwards.
	tail, _ := s.Query(LogQuery{After: 29})
	if len(tail.Logs) != 2 || tail.Logs[0].ID != 31 {
		t.Errorf("after 29 = %+v", tail.Logs)
	}

	buckets, err := s.Histogram(LogQuery{From: base, To: base.Add(30 * time.Second)}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 4 || buckets[0].Total != 10 || buckets[0].Levels.Error != 1 || buckets[0].Levels.Info != 9 || buckets[3].Total != 0 {
		t.Errorf("histogram = %+v", buckets)
	}

	st, err := s.Stats()
	if err != nil || st.Records != 31 || st.Segments != 1 || st.Bytes == 0 || st.Oldest == nil || !st.Oldest.Equal(base) {
		t.Errorf("stats = %+v, %v", st, err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if page, _ := s.Query(LogQuery{}); len(page.Logs) != 0 {
		t.Errorf("after clear = %d records", len(page.Logs))
	}
}

func TestLogStoreRotatesAndDropsOldSegments(t *testing.T) {
	s := newTestLogStore(t)
	s.segmentBytes, s.maxBytes = 2000, 6000
	for i := range 200 {
		if _, err := s.Add(LogRecord{Message: strings.Repeat("x", 100), Attrs: []LogAttr{{"i", string(rune('0' + i%10))}}}); err != nil {
			t.Fatal(err)
		}
	}
	segs, err := s.segments()
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, seg := range segs {
		total += seg.size
	}
	if len(segs) < 2 || total > 6000 {
		t.Errorf("segments = %d, %d bytes", len(segs), total)
	}
	page, _ := s.Query(LogQuery{Limit: 1000})
	if len(page.Logs) == 0 || page.Logs[0].ID != 200 || len(page.Logs) >= 200 {
		t.Errorf("kept %d records, newest %d", len(page.Logs), page.Logs[0].ID)
	}
	entries, _ := os.ReadDir(filepath.Join(s.dir, logsDir))
	if len(entries) != len(segs) {
		t.Errorf("files = %d, segments = %d", len(entries), len(segs))
	}
}

func TestLogStoreErrorsGroupByFingerprint(t *testing.T) {
	s := newTestLogStore(t)
	for i := range 3 {
		s.Ingest("app", `{"time":"2026-09-16T10:00:0`+string(rune('0'+i))+`Z","level":"ERROR","msg":"send mail to user usr_`+strings.Repeat("a", 8)+string(rune('0'+i))+`","error":"smtp 421 too busy\nat mail.go:12","request_id":"req_`+string(rune('0'+i))+`"}`)
	}
	s.Ingest("app", `{"time":"2026-09-16T10:00:05Z","level":"WARN","msg":"slow query took 812 ms","query_id":"1"}`)
	s.Ingest("app", `{"time":"2026-09-16T10:00:06Z","level":"INFO","msg":"fine"}`)
	groups, err := s.Errors(LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	slow, mail := groups[0], groups[1]
	if slow.Count != 1 || slow.Shape != "slow query took <n> ms" || slow.Level != "WARN" {
		t.Errorf("slow = %+v", slow)
	}
	if mail.Count != 3 || mail.Shape != "send mail to user <id>" || mail.Top != `smtp <n> too busy` || mail.Last.Attr("request_id") != "req_2" || !mail.FirstSeen.Before(mail.LastSeen) || mail.Fingerprint == "" {
		t.Errorf("mail = %+v", mail)
	}
}

func TestLogStoreSavedFiltersAndSubscribe(t *testing.T) {
	s := newTestLogStore(t)
	if err := s.SaveFilter(SavedFilter{Name: "bad name!", Query: json.RawMessage(`{}`)}); err == nil {
		t.Error("a bad name was accepted")
	}
	if err := s.SaveFilter(SavedFilter{Name: "errors", Query: json.RawMessage(`{"min_level":"ERROR"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFilter(SavedFilter{Name: "errors", Query: json.RawMessage(`{"min_level":"WARN"}`)}); err != nil {
		t.Fatal(err)
	}
	filters, _ := s.Filters()
	if len(filters) != 1 || string(filters[0].Query) != `{"min_level":"WARN"}` || filters[0].Saved.IsZero() {
		t.Errorf("filters = %+v", filters)
	}
	if err := s.DeleteFilter("errors"); err != nil {
		t.Fatal(err)
	}
	if filters, _ := s.Filters(); len(filters) != 0 {
		t.Errorf("after delete = %+v", filters)
	}

	c, stop := s.Subscribe()
	defer stop()
	s.Ingest("app", "hello")
	select {
	case r := <-c:
		if r.Raw != "hello" {
			t.Errorf("subscribed record = %+v", r)
		}
	case <-time.After(time.Second):
		t.Error("no record delivered")
	}
}

func TestRenderText(t *testing.T) {
	got := RenderText(`{"time":"2026-09-16T10:00:00Z","level":"INFO","msg":"http request","status":200,"path":"/a b","err":null}`)
	if !strings.Contains(got, `level=INFO msg="http request"`) || !strings.Contains(got, `status=200`) || !strings.Contains(got, `path="/a b"`) || !strings.HasPrefix(got, "time=2026-09-16T") {
		t.Errorf("RenderText = %q", got)
	}
	if RenderText("plain") != "plain" || RenderText(`{"not":"a log"}`) != `{"not":"a log"}` {
		t.Error("non-log lines changed")
	}
}

func TestRenderPretty(t *testing.T) {
	req := `{"time":"2026-09-17T04:23:35.643366+03:00","level":"INFO","msg":"http request","service":"portal-demo","source":"http","method":"GET","path":"/ops/settings","route":"/ops/settings","status":200,"duration_ms":2,"bytes":13267,"request_id":"req_1ea7d5be455144b9","trace_id":"b5ef922fa515b1d2ff3ab2cd42c248bc","span_id":"6debdb9ef5d25efd","user_id":"usr_1"}`
	got := RenderPretty(req, false)
	for _, want := range []string{"INFO  GET /ops/settings → 200 · 2 ms · 13 KB · req_1ea7d5be455144b9", "user_id=usr_1"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderPretty = %q, want %q", got, want)
		}
	}
	for _, dont := range []string{"trace_id", "span_id", "service=", "source=", "route="} {
		if strings.Contains(got, dont) {
			t.Errorf("RenderPretty = %q, shouldn't mention %s", got, dont)
		}
	}
	if colored := RenderPretty(req, true); !strings.Contains(colored, "\x1b[32m200\x1b[0m") {
		t.Errorf("colored = %q", colored)
	}
	warn := RenderPretty(`{"time":"2026-09-17T04:23:35Z","level":"WARN","msg":"job ran","job":"heartbeat","job_id":7,"service":"x"}`, false)
	if !strings.HasSuffix(warn, "WARN  job ran  job=heartbeat  job_id=7") {
		t.Errorf("warn = %q", warn)
	}
	if RenderPretty("plain line", false) != "plain line" {
		t.Error("a plain line changed")
	}

	// The store's writer ingests lines as they arrive.
	s := newTestLogStore(t)
	w := s.Writer("app")
	_, _ = w.Write([]byte(req[:40]))
	_, _ = w.Write([]byte(req[40:] + "\nplain\n"))
	page, _ := s.Query(LogQuery{})
	if len(page.Logs) != 2 || page.Logs[1].Source != SourceHTTP || page.Logs[0].Raw != "plain" {
		t.Errorf("writer ingested %+v", page.Logs)
	}
}
