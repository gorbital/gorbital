package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrBothSet reports that both KEY and KEY_FILE are set.
var ErrBothSet = errors.New("config: both variable and _FILE variant are set")

// Source reads configuration values. The zero value is not usable; use [OS]
// or construct one with test doubles.
type Source struct {
	Getenv   func(key string) string
	ReadFile func(name string) ([]byte, error)
}

// OS reads the process environment and the filesystem.
var OS = Source{Getenv: os.Getenv, ReadFile: os.ReadFile}

// Get returns the value of key. If key is empty and key+"_FILE" names a
// file, Get returns the file's contents with surrounding whitespace removed;
// this supports Docker and Kubernetes secrets mounted as files. Setting both
// returns [ErrBothSet]. A missing key returns "".
func (s Source) Get(key string) (string, error) {
	value := s.Getenv(key)
	file := s.Getenv(key + "_FILE")
	switch {
	case value != "" && file != "":
		return "", fmt.Errorf("%w: %s", ErrBothSet, key)
	case file != "":
		b, err := s.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("config: read %s_FILE: %w", key, err)
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return value, nil
	}
}

// Secret is like [Source.Get] but wraps the value in a [Secret].
func (s Source) Secret(key string) (Secret, error) {
	v, err := s.Get(key)
	return NewSecret(v), err
}
