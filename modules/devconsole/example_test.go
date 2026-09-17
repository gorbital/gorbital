package devconsole_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/modules/devconsole"
)

// An extension serves more development endpoints under /_dev/, behind the
// console's Host, loopback, forwarding-header and token checks, as sign-in's
// tests do at /_dev/auth/test/.
func ExampleExtension() {
	token := "0123456789abcdef0123456789abcdef-example"
	tests := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"path":"`+r.URL.Path+`"}`)
	})
	console, err := devconsole.New(token, devconsole.WithAddr("127.0.0.1:8080"), devconsole.WithSources(devconsole.Sources{
		Extensions: []devconsole.Extension{{Prefix: "/_dev/auth/test/", Handler: tests}},
	}))
	if err != nil {
		panic(err)
	}
	handler := console.Mount(http.NotFoundHandler(), slog.New(slog.DiscardHandler))

	request := func(withToken bool) int {
		req := httptest.NewRequest(http.MethodGet, "/_dev/auth/test/results/slt_1", nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8080", "127.0.0.1:50000"
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	fmt.Println(request(true), request(false))
	// Output: 200 401
}
