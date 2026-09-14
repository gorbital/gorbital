package settings

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations holds the module's goose migrations: the settings_values and
// settings_history tables. Apps copy them into db/migrations (the settings
// recipe does this); tests can apply them directly with pgtest.
var Migrations fs.FS = mustSub(migrationFiles, "migrations")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
