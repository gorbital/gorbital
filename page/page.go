// Package page provides cursor pagination and sorting for list endpoints:
// bounded limits, opaque cursors and allowlisted sort fields.
//
// Embed [Params] in a Huma input struct, or call [Parse] with URL query
// values, then convert to a validated [Request] against the endpoint's
// [Options].
//
// Cursors are opaque but not signed. They must only encode positions (for
// example the last item's sort key and ID); repositories still apply
// authorisation and tenant filters to every query.
//
// Stability: pre-1.0 (ADR-0015).
package page

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Limits used when [Options] leaves them zero.
const (
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxCursorLength bounds cursor size to limit abuse.
	MaxCursorLength = 512
)

// Errors returned for invalid pagination input.
var (
	ErrInvalidLimit  = errors.New("page: invalid limit")
	ErrInvalidCursor = errors.New("page: invalid cursor")
	ErrInvalidSort   = errors.New("page: invalid sort")
)

// Params are the raw query parameters of a list endpoint. The struct tags
// make Huma document and validate them.
type Params struct {
	Limit  int    `query:"limit" minimum:"0" maximum:"100" doc:"Maximum number of items to return (default 20, max 100)"`
	Cursor string `query:"cursor" maxLength:"512" doc:"Opaque cursor from a previous response's next_cursor"`
	Sort   string `query:"sort" maxLength:"200" doc:"Comma-separated sort fields; prefix a field with - for descending order" example:"-created_at"`
}

// SortField is one validated sort key.
type SortField struct {
	Field string
	Desc  bool
}

// Options configure what a list endpoint accepts.
type Options struct {
	DefaultLimit int // zero means DefaultLimit
	MaxLimit     int // zero means MaxLimit
	// SortFields is the allowlist of sortable fields. Empty means sorting is not allowed.
	SortFields []string
	// DefaultSort applies when the request has no sort.
	DefaultSort []SortField
}

// Request is a validated pagination request.
type Request struct {
	Limit  int
	Cursor string
	Sort   []SortField
}

// Parse reads limit, cursor and sort from URL query values.
func Parse(q url.Values, opts Options) (Request, error) {
	p := Params{Cursor: q.Get("cursor"), Sort: q.Get("sort")}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Request{}, fmt.Errorf("%w: %q is not a number", ErrInvalidLimit, raw)
		}
		p.Limit = n
	}
	return p.Request(opts)
}

// Request validates p against opts.
func (p Params) Request(opts Options) (Request, error) {
	def, max := opts.DefaultLimit, opts.MaxLimit
	if def <= 0 {
		def = DefaultLimit
	}
	if max <= 0 {
		max = MaxLimit
	}

	r := Request{Limit: p.Limit, Cursor: p.Cursor}
	switch {
	case r.Limit == 0:
		r.Limit = def
	case r.Limit < 0 || r.Limit > max:
		return Request{}, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, max)
	}
	if len(r.Cursor) > MaxCursorLength {
		return Request{}, fmt.Errorf("%w: too long", ErrInvalidCursor)
	}

	if strings.TrimSpace(p.Sort) == "" {
		r.Sort = slices.Clone(opts.DefaultSort)
		return r, nil
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(p.Sort, ",") {
		part = strings.TrimSpace(part)
		f := SortField{Field: strings.TrimPrefix(part, "-"), Desc: strings.HasPrefix(part, "-")}
		if !slices.Contains(opts.SortFields, f.Field) {
			return Request{}, fmt.Errorf("%w: %q is not sortable (allowed: %s)", ErrInvalidSort, f.Field, strings.Join(opts.SortFields, ", "))
		}
		if seen[f.Field] {
			return Request{}, fmt.Errorf("%w: %q appears more than once", ErrInvalidSort, f.Field)
		}
		seen[f.Field] = true
		r.Sort = append(r.Sort, f)
	}
	return r, nil
}

// EncodeCursor returns an opaque cursor for position, which must be
// JSON-encodable.
func EncodeCursor(position any) (string, error) {
	b, err := json.Marshal(position)
	if err != nil {
		return "", fmt.Errorf("page: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeCursor decodes a cursor produced by [EncodeCursor] into position.
func DecodeCursor(cursor string, position any) error {
	if len(cursor) > MaxCursorLength {
		return fmt.Errorf("%w: too long", ErrInvalidCursor)
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	if err := json.Unmarshal(b, position); err != nil {
		return fmt.Errorf("%w: malformed", ErrInvalidCursor)
	}
	return nil
}

// Result is a page of items. NextCursor is empty on the last page.
type Result[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}
