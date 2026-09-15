package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/releases"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// ReleaseResponse is one build: every instance start with the same version
// and commit.
type ReleaseResponse struct {
	Version        string    `json:"version" example:"v1.4.0"`
	Commit         string    `json:"commit,omitempty" example:"3f9a1c2b7d4e8a90"`
	FirstStartedAt time.Time `json:"first_started_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	Running        int       `json:"running" doc:"Instances running this release now"`
	Starts         int       `json:"starts" doc:"Recorded instance starts, including stopped ones"`
	Modified       bool      `json:"modified" doc:"An instance ran a build with uncommitted changes"`
}

// ReleasePage is a page of releases, newest first.
type ReleasePage struct {
	Releases   []ReleaseResponse `json:"releases"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// InstanceResponse is one start of an app instance.
type InstanceResponse struct {
	ID         int64      `json:"id"`
	InstanceID string     `json:"instance_id" doc:"Random per process start"`
	Version    string     `json:"version" example:"v1.4.0"`
	Commit     string     `json:"commit,omitempty" example:"3f9a1c2b7d4e8a90"`
	BuildTime  *time.Time `json:"build_time,omitempty"`
	Modified   bool       `json:"modified" doc:"Built with uncommitted changes"`
	GoVersion  string     `json:"go_version,omitempty" example:"go1.26.1"`
	Host       string     `json:"host,omitempty" example:"acme-api-7d9f8-x2kq"`
	StartedAt  time.Time  `json:"started_at"`
	LastSeenAt time.Time  `json:"last_seen_at" doc:"Last heartbeat"`
	StoppedAt  *time.Time `json:"stopped_at,omitempty" doc:"Set when the instance shut down cleanly"`
	Running    bool       `json:"running" doc:"Not stopped, with a heartbeat in the last 90 seconds"`
}

// InstancePage is a page of instance starts, newest first.
type InstancePage struct {
	Instances  []InstanceResponse `json:"instances"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// CurrentReleaseResponse is a release with the instances running it now.
type CurrentReleaseResponse struct {
	Version   string             `json:"version" example:"v1.4.0"`
	Commit    string             `json:"commit,omitempty" example:"3f9a1c2b7d4e8a90"`
	Instances []InstanceResponse `json:"instances"`
}

// CurrentReleases are the releases running now. There is more than one during
// a rolling deploy.
type CurrentReleases struct {
	Releases []CurrentReleaseResponse `json:"releases"`
}

type releasePageOutput struct{ Body ReleasePage }

type instancePageOutput struct{ Body InstancePage }

type currentReleasesOutput struct{ Body CurrentReleases }

type listReleasesInput struct {
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor string `query:"cursor" maxLength:"512" doc:"next_cursor from the previous page"`
}

type listInstancesInput struct {
	Version string `query:"version" maxLength:"100" example:"v1.4.0"`
	Commit  string `query:"commit" maxLength:"64"`
	Running bool   `query:"running" doc:"Only instances running now"`
	Limit   int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Cursor  string `query:"cursor" maxLength:"30" doc:"next_cursor from the previous page"`
}

type releasesHandler struct {
	svc *opsusecase.Service
}

// RegisterReleases adds the release monitor operations to api (ADR-0040).
func RegisterReleases(api huma.API, svc *opsusecase.Service) {
	h := &releasesHandler{svc: svc}
	tags := []string{"Ops: releases"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden}

	huma.Register(api, huma.Operation{
		OperationID: "ops-list-releases", Method: http.MethodGet, Path: "/ops/releases",
		Summary:     "List releases, newest first",
		Description: "Every version and commit an instance has run, with how many instances run it now.",
		Tags:        tags, Security: openapi.Bearer, Errors: append([]int{http.StatusBadRequest}, errs...),
	}, h.list)
	huma.Register(api, huma.Operation{
		OperationID: "ops-current-releases", Method: http.MethodGet, Path: "/ops/releases/current",
		Summary:     "List the releases running now",
		Description: "More than one during a rolling deploy. An instance counts as running until it stops or misses three heartbeats.",
		Tags:        tags, Security: openapi.Bearer, Errors: errs,
	}, h.current)
	huma.Register(api, huma.Operation{
		OperationID: "ops-list-release-instances", Method: http.MethodGet, Path: "/ops/releases/instances",
		Summary: "List instance starts, newest first",
		Tags:    tags, Security: openapi.Bearer, Errors: append([]int{http.StatusBadRequest}, errs...),
	}, h.instances)
}

func (h *releasesHandler) list(ctx context.Context, in *listReleasesInput) (*releasePageOutput, error) {
	page, err := h.svc.ListReleases(ctx, releases.ReleaseFilter{Limit: in.Limit, Cursor: in.Cursor})
	if err != nil {
		return nil, err
	}
	out := &releasePageOutput{Body: ReleasePage{Releases: make([]ReleaseResponse, len(page.Releases)), NextCursor: page.NextCursor}}
	for i, r := range page.Releases {
		out.Body.Releases[i] = ReleaseResponse{
			Version: r.Version, Commit: r.Commit, FirstStartedAt: r.FirstStartedAt, LastSeenAt: r.LastSeenAt,
			Running: r.Running, Starts: r.Starts, Modified: r.Modified,
		}
	}
	return out, nil
}

func (h *releasesHandler) current(ctx context.Context, _ *struct{}) (*currentReleasesOutput, error) {
	current, err := h.svc.CurrentReleases(ctx)
	if err != nil {
		return nil, err
	}
	out := &currentReleasesOutput{Body: CurrentReleases{Releases: make([]CurrentReleaseResponse, len(current))}}
	for i, r := range current {
		out.Body.Releases[i] = CurrentReleaseResponse{Version: r.Version, Commit: r.Commit, Instances: instanceResponses(r.Instances)}
	}
	return out, nil
}

func (h *releasesHandler) instances(ctx context.Context, in *listInstancesInput) (*instancePageOutput, error) {
	page, err := h.svc.ListReleaseInstances(ctx, releases.InstanceFilter{
		Version: in.Version, Commit: in.Commit, RunningOnly: in.Running, Limit: in.Limit, Cursor: in.Cursor,
	})
	if err != nil {
		return nil, err
	}
	return &instancePageOutput{Body: InstancePage{Instances: instanceResponses(page.Instances), NextCursor: page.NextCursor}}, nil
}

func instanceResponses(instances []releases.Instance) []InstanceResponse {
	out := make([]InstanceResponse, len(instances))
	for i, in := range instances {
		out[i] = InstanceResponse{
			ID: in.ID, InstanceID: in.InstanceID, Version: in.Version, Commit: in.Commit, BuildTime: in.BuildTime,
			Modified: in.Modified, GoVersion: in.GoVersion, Host: in.Host, StartedAt: in.StartedAt,
			LastSeenAt: in.LastSeenAt, StoppedAt: in.StoppedAt, Running: in.Running,
		}
	}
	return out
}
