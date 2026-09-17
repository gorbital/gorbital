package gorbital

import (
	"strings"
	"testing"
)

func TestJoinPath(t *testing.T) {
	tests := []struct {
		prefix, path string
		want         string
		wantErr      bool
	}{
		{"", "", "", false},
		{"/v1/books", "", "/v1/books", false},
		{"", "/v1/books", "/v1/books", false},
		{"/v1", "/books/{id}", "/v1/books/{id}", false},
		{"v1", "/books", "", true},
		{"/v1", "books", "", true},
		{"/v1/", "/books", "", true},
		{"/v1", "/books/", "", true},
		{"/v1//books", "", "", true},
		{"/", "", "", true},
	}
	for _, tt := range tests {
		got, err := joinPath(tt.prefix, tt.path)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("joinPath(%q, %q) = %q, %v; want %q, error %v", tt.prefix, tt.path, got, err, tt.want, tt.wantErr)
		}
	}
}

// FuzzJoinPath checks that every joined path the router accepts is a clean
// absolute path: it starts with a slash unless empty, and has no empty
// segment or trailing slash that would make two spellings of one route.
func FuzzJoinPath(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"/v1", "/books/{id}"}, {"/v1/", "/x"}, {"v1", ""}, {"/a//b", "/c"}, {"/", "/"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, prefix, path string) {
		got, err := joinPath(prefix, path)
		if err != nil {
			if got != "" {
				t.Fatalf("joinPath(%q, %q) returned %q with an error", prefix, path, got)
			}
			return
		}
		if got == "" {
			return
		}
		if !strings.HasPrefix(got, "/") || strings.HasSuffix(got, "/") || strings.Contains(got, "//") {
			t.Fatalf("joinPath(%q, %q) = %q, not a clean absolute path", prefix, path, got)
		}
		if got != prefix+path {
			t.Fatalf("joinPath(%q, %q) = %q, want the concatenation", prefix, path, got)
		}
	})
}
