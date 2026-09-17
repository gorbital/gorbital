// Package ui embeds the built Dev Portal UI: the static export of
// gorbital-dashboards/apps/devtools, copied into dist/ by
// scripts/sync-portal.sh (ADR-0066). The repository holds only dist/.gitkeep,
// so a plain checkout builds an orb that serves a placeholder page instead;
// release builds run the script first.
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
