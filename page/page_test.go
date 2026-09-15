package page_test

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gorbital.dev/page"
)

func TestParse(t *testing.T) {
	opts := page.Options{
		SortFields:  []string{"created_at", "name"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
	tests := []struct {
		query   string
		want    page.Request
		wantErr error
	}{
		{"", page.Request{Limit: 20, Sort: []page.SortField{{Field: "created_at", Desc: true}}}, nil},
		{"limit=50&cursor=abc", page.Request{Limit: 50, Cursor: "abc", Sort: []page.SortField{{Field: "created_at", Desc: true}}}, nil},
		{"sort=name,-created_at", page.Request{Limit: 20, Sort: []page.SortField{{Field: "name"}, {Field: "created_at", Desc: true}}}, nil},
		{"limit=101", page.Request{}, page.ErrInvalidLimit},
		{"limit=-1", page.Request{}, page.ErrInvalidLimit},
		{"limit=ten", page.Request{}, page.ErrInvalidLimit},
		{"sort=password_hash", page.Request{}, page.ErrInvalidSort},
		{"sort=name,-name", page.Request{}, page.ErrInvalidSort},
		{"cursor=" + strings.Repeat("a", page.MaxCursorLength+1), page.Request{}, page.ErrInvalidCursor},
	}
	for _, tt := range tests {
		q, _ := url.ParseQuery(tt.query)
		got, err := page.Parse(q, opts)
		if tt.wantErr != nil {
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want %v", tt.query, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, nil", tt.query, got, err, tt.want)
		}
	}
}

func TestParamsCustomLimits(t *testing.T) {
	got, err := page.Params{}.Request(page.Options{DefaultLimit: 5, MaxLimit: 10})
	if err != nil || got.Limit != 5 {
		t.Errorf("Request(default 5) = %+v, %v; want limit 5", got, err)
	}
	if _, err := (page.Params{Limit: 11}).Request(page.Options{MaxLimit: 10}); !errors.Is(err, page.ErrInvalidLimit) {
		t.Errorf("Request(limit 11, max 10) error = %v, want ErrInvalidLimit", err)
	}
	if _, err := (page.Params{Sort: "name"}).Request(page.Options{}); !errors.Is(err, page.ErrInvalidSort) {
		t.Errorf("Request(sort without allowlist) error = %v, want ErrInvalidSort", err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	type position struct {
		CreatedAt string `json:"c"`
		ID        string `json:"i"`
	}
	in := position{CreatedAt: "2026-09-14T12:00:00Z", ID: "prj_1"}
	cursor, err := page.EncodeCursor(in)
	if err != nil {
		t.Fatalf("EncodeCursor(%+v) error = %v", in, err)
	}
	var out position
	if err := page.DecodeCursor(cursor, &out); err != nil || out != in {
		t.Errorf("DecodeCursor(EncodeCursor(%+v)) = %+v, %v", in, out, err)
	}
	for _, bad := range []string{"%%%", "bm90LWpzb24", strings.Repeat("a", page.MaxCursorLength+1)} {
		if err := page.DecodeCursor(bad, &out); !errors.Is(err, page.ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%.20q) error = %v, want ErrInvalidCursor", bad, err)
		}
	}
}
