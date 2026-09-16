package devconsole

import (
	"encoding/json"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// App describes the running app: GET /_dev/app.
type App struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	GoVersion string `json:"go_version"`
	// Env is APP_ENV.
	Env string `json:"env"`
	// Libraries are the gorbital library modules linked into the binary.
	Libraries []Library `json:"libraries"`
	// Modules are the API's areas: the OpenAPI document's tags.
	Modules []string `json:"modules"`
	// Jobs, Settings, Flags and Permissions are empty in apps without them.
	Jobs        []Job               `json:"jobs"`
	Settings    []Setting           `json:"settings"`
	Flags       []Flag              `json:"flags"`
	Permissions []PermissionCatalog `json:"permissions"`
}

// Library is a linked Go module.
type Library struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	// Replaced reports a module replaced by a local directory or another
	// module, as in a gorbital checkout.
	Replaced bool `json:"replaced,omitempty"`
}

// Libraries returns the gorbital.dev modules linked into the running
// binary, sorted by path.
func Libraries() []Library {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return []Library{}
	}
	return librariesOf(bi)
}

func librariesOf(bi *debug.BuildInfo) []Library {
	out := []Library{}
	for _, m := range bi.Deps {
		if m.Path != "gorbital.dev" && !strings.HasPrefix(m.Path, "gorbital.dev/") {
			continue
		}
		out = append(out, Library{Path: m.Path, Version: m.Version, Replaced: m.Replace != nil})
	}
	slices.SortFunc(out, func(a, b Library) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// Job is a declared job definition and its effective configuration.
type Job struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	// Schedule is a cron expression or descriptor; empty for on-demand jobs.
	Schedule  string     `json:"schedule"`
	Modified  bool       `json:"modified"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
}

// Setting is a declared runtime setting and its current value.
type Setting struct {
	Key            string          `json:"key"`
	Group          string          `json:"group"`
	Description    string          `json:"description"`
	Kind           string          `json:"kind"`
	Value          json.RawMessage `json:"value"`
	Default        json.RawMessage `json:"default"`
	Modified       bool            `json:"modified"`
	OrgOverridable bool            `json:"org_overridable"`
}

// Flag is a declared feature flag and its current state, with target lists
// summarised as counts.
type Flag struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Client      bool   `json:"client"`
	Enabled     bool   `json:"enabled"`
	Default     bool   `json:"default"`
	// Percentage is the rollout percentage; nil without a rollout.
	Percentage *int `json:"percentage"`
	// Targets counts the organisation and user IDs in allow and deny lists.
	Targets  int  `json:"targets"`
	Modified bool `json:"modified"`
}

// PermissionCatalog is a permission catalog: its permissions and the roles
// granting them.
type PermissionCatalog struct {
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions"`
	Roles       []Role       `json:"roles"`
}

// Permission is a declared permission.
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Role is a declared role.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// Migrations is the database's migration state: GET /_dev/migrations.
type Migrations struct {
	// Current is the highest applied version, Latest the newest migration
	// file's, Pending the files not applied yet.
	Current int64 `json:"current"`
	Latest  int64 `json:"latest"`
	Pending int   `json:"pending"`
}

// JobRun is a job, without its arguments: GET /_dev/jobs.
type JobRun struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	// Errors are the failed attempts' messages, oldest first.
	Errors    []string `json:"errors"`
	RequestID string   `json:"request_id,omitempty"`
}
