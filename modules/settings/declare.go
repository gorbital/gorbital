package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Bool declares a true/false setting.
func Bool(r *Registry, key string, def bool, opts ...Option) *Setting[bool] {
	return declare(r, key, KindBool, def, decodeJSON[bool]("a boolean"), encodeJSON[bool], opts)
}

// Int declares a whole-number setting. Combine with [Range].
func Int(r *Registry, key string, def int, opts ...Option) *Setting[int] {
	return declare(r, key, KindInt, def, decodeJSON[int]("a whole number"), encodeJSON[int], opts)
}

// Float declares a decimal setting. Combine with [Range], passing float
// bounds such as Range(0.0, 2.0).
func Float(r *Registry, key string, def float64, opts ...Option) *Setting[float64] {
	return declare(r, key, KindFloat, def, decodeJSON[float64]("a number"), encodeJSON[float64], opts)
}

// String declares a text setting. Combine with [MaxLen], [URL] or [Email].
func String(r *Registry, key string, def string, opts ...Option) *Setting[string] {
	return declare(r, key, KindString, def, decodeJSON[string]("a string"), encodeJSON[string], opts)
}

// Enum declares a text setting limited to allowed values.
func Enum(r *Registry, key string, def string, allowed []string, opts ...Option) *Setting[string] {
	opts = append([]Option{OneOf(allowed...)}, opts...)
	return declare(r, key, KindEnum, def, decodeJSON[string]("a string"), encodeJSON[string], opts)
}

// Duration declares a duration setting, stored and edited as a Go duration
// string such as "15m" or "1h30m". Combine with [Range].
func Duration(r *Registry, key string, def time.Duration, opts ...Option) *Setting[time.Duration] {
	return declare(r, key, KindDuration, def, decodeDuration, encodeDuration, opts)
}

// StringList declares a list-of-text setting, such as allowed origins.
// Get returns a copy the caller may modify.
func StringList(r *Registry, key string, def []string, opts ...Option) *Setting[[]string] {
	if def == nil {
		def = []string{}
	}
	return declare(r, key, KindStringList, slices.Clone(def), decodeStringList, encodeJSON[[]string], opts)
}

func decodeJSON[T any](what string) func(json.RawMessage) (T, error) {
	return func(raw json.RawMessage) (T, error) {
		var v T
		if isNull(raw) {
			return v, errors.New("a value is required")
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return v, fmt.Errorf("must be %s", what)
		}
		return v, nil
	}
}

func encodeJSON[T any](v T) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// Only bool, int, float64, string and []string are encoded; a
		// failure means a NaN or infinite float default.
		panic(fmt.Sprintf("settings: encode %v: %v", v, err))
	}
	return b
}

func decodeDuration(raw json.RawMessage) (time.Duration, error) {
	s, err := decodeJSON[string]("a duration such as \"15m\"")(raw)
	if err != nil {
		return 0, err
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New(`must be a duration such as "15m" or "1h30m"`)
	}
	return d, nil
}

func encodeDuration(d time.Duration) json.RawMessage { return encodeJSON(d.String()) }

func decodeStringList(raw json.RawMessage) ([]string, error) {
	list, err := decodeJSON[[]string]("a list of strings")(raw)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []string{}
	}
	return list, nil
}

func isNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
