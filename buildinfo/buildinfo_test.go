package buildinfo

import (
	"encoding/json"
	"net/http/httptest"
	"runtime/debug"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		GoVersion: "go1.25.3",
		Main:      debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-09-14T12:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	tests := []struct {
		name     string
		bi       *debug.BuildInfo
		override string
		want     Info
	}{
		{"no build info", nil, "", Info{Version: "dev"}},
		{"devel build with vcs", bi, "", Info{Version: "dev", Commit: "abc123", BuildTime: "2026-09-14T12:00:00Z", Modified: true, GoVersion: "go1.25.3"}},
		{"link-time version wins", bi, "v1.2.3", Info{Version: "v1.2.3", Commit: "abc123", BuildTime: "2026-09-14T12:00:00Z", Modified: true, GoVersion: "go1.25.3"}},
		{"module version", &debug.BuildInfo{GoVersion: "go1.25.3", Main: debug.Module{Version: "v0.1.0"}}, "", Info{Version: "v0.1.0", GoVersion: "go1.25.3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromBuildInfo(tt.bi, tt.override); got != tt.want {
				t.Errorf("fromBuildInfo(%s) = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/version", nil))
	var got Info
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Handler() body %q is not JSON: %v", rec.Body.String(), err)
	}
	if rec.Code != 200 || got.Version == "" {
		t.Errorf("Handler() = %d %+v, want 200 with a version", rec.Code, got)
	}
}
