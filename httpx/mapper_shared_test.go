package httpx_test

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"apistock.dev/httpx"
)

func TestMapperSharesCodesWithTheSameStatus(t *testing.T) {
	errA, errB := errors.New("module a: authentication required"), errors.New("module b: authentication required")
	m, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	err = m.Add(
		httpx.Mapping{Err: errA, Status: 401, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: errB, Status: 401, Code: "unauthenticated", Detail: "authentication is required"},
	)
	if err != nil {
		t.Fatalf("Add(two errors sharing a code and status) error = %v", err)
	}
	if err := m.Add(httpx.Mapping{Err: errors.New("c"), Status: 403, Code: "unauthenticated", Detail: "x"}); err == nil {
		t.Error("Add(same code, different status) error = nil")
	}
	if err := m.Add(httpx.Mapping{Err: errA, Status: 401, Code: "other_code", Detail: "x"}); err == nil {
		t.Error("Add(error mapped twice) error = nil")
	}
	if p, ok := m.Match(fmt.Errorf("wrapped: %w", errB)); !ok || p.Status != 401 {
		t.Errorf("Match(errB) = %+v, %t", p, ok)
	}
}
