// Package ui embeds the built Dev Portal UI: the static export of
// gorbital-dashboards/apps/devtools, copied into dist/ by
// scripts/sync-portal.sh and committed (ADR-0066), so go install and release
// builds carry the same UI; dist/BUILD names the gorbital-dashboards commit.
// A build whose dist/ has no index.html serves a placeholder page instead.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI, rooted at its index.html.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded above
	}
	return sub
}
