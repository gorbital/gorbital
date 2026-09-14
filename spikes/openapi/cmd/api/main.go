// The api command runs the spike server, or prints the OpenAPI document with
// "api openapi".
package main

import (
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"apistock.dev/spikes/openapi/internal/app"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler, api := app.New(logger)

	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(api.OpenAPI())
	}

	srv := &http.Server{Addr: "127.0.0.1:8088", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("listening", "addr", "http://"+srv.Addr, "docs", "http://"+srv.Addr+"/docs")
	return srv.ListenAndServe()
}
