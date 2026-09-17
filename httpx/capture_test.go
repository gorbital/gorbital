package httpx_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorbital.dev/httpx"
)

func TestCapture(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantStatus  int
		wantBytes   int64
		wantHeaders bool
	}{
		{"nothing written", func(http.ResponseWriter, *http.Request) {}, http.StatusOK, 0, false},
		{"body only", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) }, http.StatusOK, 5, true},
		{"status then body", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{}"))
		}, http.StatusCreated, 2, true},
		{"first status wins", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusTeapot, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := httpx.Capture(httptest.NewRecorder())
			tt.handler(cw, httptest.NewRequest(http.MethodGet, "/", nil))
			if cw.Status() != tt.wantStatus || cw.Bytes() != tt.wantBytes || cw.WroteHeader() != tt.wantHeaders {
				t.Errorf("Status, Bytes, WroteHeader = %d, %d, %t; want %d, %d, %t",
					cw.Status(), cw.Bytes(), cw.WroteHeader(), tt.wantStatus, tt.wantBytes, tt.wantHeaders)
			}
		})
	}
}

func TestCaptureSharesOneRecord(t *testing.T) {
	cw := httpx.Capture(httptest.NewRecorder())
	if again := httpx.Capture(cw); again != cw {
		t.Fatal("Capture of a *Captured returned a new wrapper")
	}
	if cw.Unwrap() == nil {
		t.Fatal("Unwrap returned nil")
	}
}

func ExampleCapture() {
	// logErrors logs every response with a 5xx status.
	logErrors := func(logger *slog.Logger) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cw := httpx.Capture(w)
				next.ServeHTTP(cw, r)
				if cw.Status() >= 500 {
					logger.ErrorContext(r.Context(), "server error", "status", cw.Status(), "bytes", cw.Bytes())
				}
			})
		}
	}

	failing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	})
	rec := httptest.NewRecorder()
	logErrors(slog.New(slog.DiscardHandler))(failing).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/books", nil))
	fmt.Println(rec.Code)
	// Output:
	// 503
}

func ExampleCaptured() {
	cw := httpx.Capture(httptest.NewRecorder())
	cw.WriteHeader(http.StatusCreated)
	_, _ = cw.Write([]byte(`{"id":"bok_1"}`))
	fmt.Println(cw.Status(), cw.Bytes(), cw.WroteHeader())
	// Output:
	// 201 14 true
}

func ExampleCaptured_Status() {
	cw := httpx.Capture(httptest.NewRecorder())
	fmt.Println(cw.Status()) // nothing written yet: 200, as net/http sends
	cw.WriteHeader(http.StatusNotFound)
	fmt.Println(cw.Status())
	// Output:
	// 200
	// 404
}

func ExampleCaptured_WroteHeader() {
	// A middleware that sets a header only while it still can.
	addVersion := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := httpx.Capture(w)
			next.ServeHTTP(cw, r)
			if !cw.WroteHeader() {
				cw.Header().Set("X-Version", "1")
				cw.WriteHeader(http.StatusNoContent)
			}
		})
	}
	rec := httptest.NewRecorder()
	addVersion(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("X-Version"))
	// Output:
	// 204 1
}

func ExampleCaptured_Bytes() {
	cw := httpx.Capture(httptest.NewRecorder())
	_, _ = fmt.Fprint(cw, "hello, ")
	_, _ = fmt.Fprint(cw, "world")
	fmt.Println(cw.Bytes())
	// Output:
	// 12
}
