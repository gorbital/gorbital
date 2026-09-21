// Package delivery is the profiles module's HTTP adapter: the route table in
// this file, and one file per operation.
package delivery

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/shelfie/internal/modules/profiles/usecase"
)

type handlers struct {
	svc *usecase.Service
}

// Register adds the profile routes to r.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	profile := r.Group("/v1/profile", gorbital.Tags("Profiles"))
	gorbital.Get(profile, "", h.getProfile,
		gorbital.Summary("Get your profile"),
		gorbital.Description("404 profile_incomplete until the reader creates it: readers who signed up with Google, Apple or GitHub complete it with PUT."),
		gorbital.Errors(404), guard.Permission(usecase.PermRead))
	gorbital.Put(profile, "", h.updateProfile,
		gorbital.Summary("Create or change your profile"), gorbital.Errors(422), guard.Permission(usecase.PermWrite))
}
