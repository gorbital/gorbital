package page_test

import (
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"gorbital.dev/page"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"", "limit=50&cursor=abc", "sort=name,-created_at", "limit=101", "limit=-1", "limit=ten",
		"sort=password_hash", "sort=name,-name", "cursor=" + strings.Repeat("a", page.MaxCursorLength+1),
		"sort=+name", "sort= , ", "limit=0&sort=-", "limit=%2B7",
	} {
		f.Add(seed)
	}
	opts := page.Options{
		SortFields:  []string{"created_at", "name"},
		DefaultSort: []page.SortField{{Field: "created_at", Desc: true}},
	}
	f.Fuzz(func(t *testing.T, query string) {
		q, _ := url.ParseQuery(query) // keeps what parsed
		got, err := page.Parse(q, opts)
		if err != nil {
			if !errors.Is(err, page.ErrInvalidLimit) && !errors.Is(err, page.ErrInvalidCursor) && !errors.Is(err, page.ErrInvalidSort) {
				t.Fatalf("Parse(%q) error = %v, want a page error", query, err)
			}
			if !reflect.DeepEqual(got, page.Request{}) {
				t.Fatalf("Parse(%q) = %+v with error, want zero Request", query, got)
			}
			return
		}
		if got.Limit < 1 || got.Limit > page.MaxLimit {
			t.Fatalf("Parse(%q).Limit = %d, want 1..%d", query, got.Limit, page.MaxLimit)
		}
		if got.Cursor != q.Get("cursor") || len(got.Cursor) > page.MaxCursorLength {
			t.Fatalf("Parse(%q).Cursor = %q, want %q within %d bytes", query, got.Cursor, q.Get("cursor"), page.MaxCursorLength)
		}
		if len(got.Sort) == 0 {
			t.Fatalf("Parse(%q).Sort is empty, want the request's sort or the default", query)
		}
		seen := map[string]bool{}
		sorts := make([]string, len(got.Sort))
		for i, s := range got.Sort {
			if !slices.Contains(opts.SortFields, s.Field) || seen[s.Field] {
				t.Fatalf("Parse(%q).Sort = %+v: field not allowed or repeated", query, got.Sort)
			}
			seen[s.Field] = true
			sorts[i] = s.Field
			if s.Desc {
				sorts[i] = "-" + s.Field
			}
		}
		// A validated request re-encoded as a query parses to itself.
		again, err := page.Parse(url.Values{
			"limit":  {strconv.Itoa(got.Limit)},
			"cursor": {got.Cursor},
			"sort":   {strings.Join(sorts, ",")},
		}, opts)
		if err != nil || !reflect.DeepEqual(again, got) {
			t.Fatalf("Parse(%q) = %+v; re-encoded = %+v, %v", query, got, again, err)
		}
	})
}

func FuzzCursorRoundTrip(f *testing.F) {
	f.Add("2026-09-14T12:00:00Z", "prj_1", int64(7))
	f.Add("", "", int64(-1))
	f.Add(strings.Repeat("x", 400), " <>&", int64(1)<<53)
	type position struct {
		CreatedAt string `json:"c"`
		ID        string `json:"i"`
		N         int64  `json:"n"`
	}
	f.Fuzz(func(t *testing.T, createdAt, id string, n int64) {
		in := position{CreatedAt: createdAt, ID: id, N: n}
		cursor, err := page.EncodeCursor(in)
		if err != nil {
			t.Fatalf("EncodeCursor(%+v) error = %v", in, err)
		}
		if strings.Trim(cursor, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			t.Fatalf("EncodeCursor(%+v) = %q, want only base64url characters", in, cursor)
		}
		var out position
		err = page.DecodeCursor(cursor, &out)
		if len(cursor) > page.MaxCursorLength {
			if !errors.Is(err, page.ErrInvalidCursor) {
				t.Fatalf("DecodeCursor(%d-byte cursor) error = %v, want ErrInvalidCursor", len(cursor), err)
			}
			return
		}
		if err != nil {
			t.Fatalf("DecodeCursor(EncodeCursor(%+v)) error = %v", in, err)
		}
		// JSON replaces invalid UTF-8, so only valid strings survive exactly.
		if utf8.ValidString(createdAt) && utf8.ValidString(id) && out != in {
			t.Fatalf("DecodeCursor(EncodeCursor(%+v)) = %+v", in, out)
		}
	})
}

func FuzzDecodeCursor(f *testing.F) {
	for _, seed := range []string{"%%%", "bm90LWpzb24", strings.Repeat("a", page.MaxCursorLength+1), "eyJjIjoiMjAyNi0wOS0xNFQxMjowMDowMFoiLCJpIjoicHJqXzEifQ", "bnVsbA", "", "MQ", "WzEsMi4wLCJcdWQ4MDAiXQ"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, cursor string) {
		var v any
		if err := page.DecodeCursor(cursor, &v); err != nil {
			if !errors.Is(err, page.ErrInvalidCursor) {
				t.Fatalf("DecodeCursor(%q) error = %v, want ErrInvalidCursor", cursor, err)
			}
			return
		}
		if len(cursor) > page.MaxCursorLength {
			t.Fatalf("DecodeCursor accepted a %d-byte cursor", len(cursor))
		}
		// Any accepted position encodes and decodes to the same value.
		again, err := page.EncodeCursor(v)
		if err != nil {
			t.Fatalf("EncodeCursor(DecodeCursor(%q) = %#v) error = %v", cursor, v, err)
		}
		if len(again) > page.MaxCursorLength {
			return // canonical JSON can be longer, such as 1e9 as 1000000000
		}
		var w any
		if err := page.DecodeCursor(again, &w); err != nil || !reflect.DeepEqual(v, w) {
			t.Fatalf("DecodeCursor(%q) = %#v; round trip = %#v, %v", cursor, v, w, err)
		}
	})
}
