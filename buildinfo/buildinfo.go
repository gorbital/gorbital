// Package buildinfo reports the version, commit and build time of the running
// binary, from Go build information or a version set at link time.
//
// Set an explicit version when building:
//
//	go build -ldflags "-X apistock.dev/buildinfo.version=v1.2.3" ./cmd/api
//
// Stability: pre-1.0 (ADR-0015).
package buildinfo

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

// version is set with -ldflags "-X apistock.dev/buildinfo.version=...".
var version string

// Info describes the running binary.
type Info struct {
	Version   string `json:"version" example:"v1.2.3"`
	Commit    string `json:"commit,omitempty" example:"3f9a1c2b7d4e"`
	BuildTime string `json:"build_time,omitempty" example:"2026-09-14T12:00:00Z"`
	Modified  bool   `json:"modified,omitempty"`
	GoVersion string `json:"go_version" example:"go1.25.3"`
}

// Read returns information about the running binary. Version is "dev" when
// neither a link-time version nor a module version is available.
func Read() Info {
	bi, _ := debug.ReadBuildInfo()
	return fromBuildInfo(bi, version)
}

func fromBuildInfo(bi *debug.BuildInfo, override string) Info {
	info := Info{Version: "dev"}
	if bi != nil {
		info.GoVersion = bi.GoVersion
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			info.Version = v
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = s.Value
			case "vcs.time":
				info.BuildTime = s.Value
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	}
	if override != "" {
		info.Version = override
	}
	return info
}

// Handler serves [Read] as JSON.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Read())
	})
}
