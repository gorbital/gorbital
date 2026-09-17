package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestHTTPMetricsIgnoreHost checks that clients can't add metric series by
// sending different Host headers (HTTP-2).
func TestHTTPMetricsIgnoreHost(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	tel := &Telemetry{
		tp: sdktrace.NewTracerProvider(),
		mp: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), httpMetricsView()),
	}
	h := tel.HTTPMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for i := range 50 {
		req := httptest.NewRequest("GET", "/livez", nil)
		req.Host = fmt.Sprintf("h%d.attacker.example:%d", i, 1000+i)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != "http.server.request.duration" {
				continue
			}
			found = true
			if len(hist.DataPoints) != 1 {
				t.Errorf("%s has %d series for 50 Host headers, want 1", m.Name, len(hist.DataPoints))
			}
			for _, dp := range hist.DataPoints {
				for _, key := range []attribute.Key{"server.address", "server.port"} {
					if v, ok := dp.Attributes.Value(key); ok {
						t.Errorf("%s has %s=%s, want none", m.Name, key, v.String())
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("no http.server.request.duration metric recorded")
	}
	_ = tel.tp.Shutdown(context.Background())
}
