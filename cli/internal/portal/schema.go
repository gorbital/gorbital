package portal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gorbital.dev/cli/internal/pgmeta"
)

// The live schema status (ADR-0080): what the database has against what
// db/migrations says, published as a "schema" event whenever either side
// changes (a file, a migrate run, the SQL editor) and served on demand at
// GET /_portal/api/db/schema-status.

// Sources of a schema status: what caused it.
const (
	// SchemaSourceStartup is the status computed when orb dev started.
	SchemaSourceStartup = "startup"
	// SchemaSourceCode: a file under db/migrations changed and orb dev did
	// not apply it (no reload, or the app is stopped or failed).
	SchemaSourceCode = "code"
	// SchemaSourceMigrate: orb dev ran migrations after a code change or a
	// restart; Applied lists the files that became applied.
	SchemaSourceMigrate = "migrate"
	// SchemaSourcePortal: a portal command ran migrations (a table-editor or
	// migration plan, apply pending, roll back, redo, reset).
	SchemaSourcePortal = "portal"
	// SchemaSourceSQL: the SQL editor committed a DDL statement.
	SchemaSourceSQL = "sql"
)

// Reasons a migration is pending.
const (
	// PendingNew: the file is newer than every applied version.
	PendingNew = "new"
	// PendingOutOfOrder: the file's version is lower than the highest
	// applied one, so goose won't apply it in order.
	PendingOutOfOrder = "out_of_order"
)

// SchemaStatus is the live schema status.
type SchemaStatus struct {
	// Database reports whether the app has one; false leaves the rest empty.
	Database bool `json:"database"`
	// Source is what caused this status: startup, code, migrate, portal or
	// sql.
	Source    string    `json:"source,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	// Applied lists the files the last migrate run applied (sources migrate,
	// portal and startup); empty otherwise.
	Applied []string `json:"applied"`
	// Pending lists the files in db/migrations the database hasn't applied.
	Pending []PendingMigration `json:"pending"`
	// Edited lists applied files whose content differs from what was
	// applied: PostgreSQL keeps the old version and goose ignores the
	// change.
	Edited []EditedMigration `json:"edited"`
	// NeedsRestart reports pending files orb dev won't apply on its own:
	// --no-reload, the app stopped or failed, or the last migrate failed.
	NeedsRestart bool `json:"needs_restart"`
	// Problem is the last migrate error, empty when fine.
	Problem string `json:"problem"`
}

// PendingMigration is a file the database hasn't applied.
type PendingMigration struct {
	File    string `json:"file"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// EditedMigration is an applied file whose content changed since.
type EditedMigration struct {
	File    string `json:"file"`
	Version string `json:"version"`
}

// SchemaInput is what ComputeSchemaStatus needs.
type SchemaInput struct {
	// Migrations are the files with their state, as pgmeta.Migrations
	// lists them.
	Migrations []pgmeta.Migration
	// Record maps each applied file to the hash of the content that was
	// applied (MigrationRecord.Applied).
	Record map[string]string
	Source string
	// Applied lists the files the run that caused this status applied.
	Applied []string
	// AutoApply reports that orb dev applies pending files on its own:
	// reload is on, the app runs and nothing failed.
	AutoApply bool
	// Problem is the last migrate error.
	Problem string
	Now     time.Time
}

// ComputeSchemaStatus compares the files with the database and the record.
func ComputeSchemaStatus(in SchemaInput) SchemaStatus {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	st := SchemaStatus{
		Database: true, Source: in.Source, CheckedAt: now.UTC(),
		Applied: append([]string{}, in.Applied...), Pending: []PendingMigration{}, Edited: []EditedMigration{},
		Problem: in.Problem,
	}
	var highest int64
	for _, m := range in.Migrations {
		if m.Applied && m.Version > highest {
			highest = m.Version
		}
	}
	for _, m := range in.Migrations {
		if m.Path == "" {
			continue // a version in the table without a file
		}
		file, version := filepath.Base(m.Path), strconv.FormatInt(m.Version, 10)
		if !m.Applied {
			reason := PendingNew
			if m.Version < highest {
				reason = PendingOutOfOrder
			}
			st.Pending = append(st.Pending, PendingMigration{File: file, Version: version, Reason: reason})
			continue
		}
		if hash, ok := in.Record[file]; ok && hash != HashSQL(m.SQL) {
			st.Edited = append(st.Edited, EditedMigration{File: file, Version: version})
		}
	}
	st.NeedsRestart = len(st.Pending) > 0 && (!in.AutoApply || in.Problem != "")
	return st
}

// HashSQL is the hash the record keeps of a migration's content.
func HashSQL(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

// MigrationRecord remembers the content of each applied migration file,
// so a file edited after it was applied is found (.orb/portal/
// migrations.json). Every successful migrate run writes it for the files
// now applied; a file seen applied for the first time is recorded with its
// current content.
type MigrationRecord struct {
	// Applied maps a file name to the SHA-256 of the content applied.
	Applied map[string]string `json:"applied"`
}

// migrationRecordFile is where the record lives inside the app.
const migrationRecordFile = portalDir + "/migrations.json"

// LoadMigrationRecord reads the app's record; a missing file is an empty
// record.
func LoadMigrationRecord(dir string) (MigrationRecord, error) {
	r := MigrationRecord{Applied: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(migrationRecordFile)))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return MigrationRecord{Applied: map[string]string{}}, err
	}
	if r.Applied == nil {
		r.Applied = map[string]string{}
	}
	return r, nil
}

// Save writes the record.
func (r MigrationRecord) Save(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(portalDir)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.FromSlash(migrationRecordFile)), append(data, '\n'), 0o644)
}

// Update returns the record for the files now applied: files in refreshed
// (just applied, or applied again) and files not yet recorded get the hash
// of their current content, the others keep the hash they have, and files
// no longer applied are dropped.
func (r MigrationRecord) Update(migrations []pgmeta.Migration, refreshed []string) MigrationRecord {
	fresh := map[string]bool{}
	for _, f := range refreshed {
		fresh[f] = true
	}
	out := MigrationRecord{Applied: map[string]string{}}
	for _, m := range migrations {
		if !m.Applied || m.Path == "" {
			continue
		}
		file := filepath.Base(m.Path)
		if hash, ok := r.Applied[file]; ok && !fresh[file] {
			out.Applied[file] = hash
			continue
		}
		out.Applied[file] = HashSQL(m.SQL)
	}
	return out
}

// AppliedFiles returns the names of the applied files.
func AppliedFiles(migrations []pgmeta.Migration) []string {
	var out []string
	for _, m := range migrations {
		if m.Applied && m.Path != "" {
			out = append(out, filepath.Base(m.Path))
		}
	}
	return out
}

// noDatabaseSchema is the status of an app without a database.
func noDatabaseSchema() SchemaStatus {
	return SchemaStatus{Applied: []string{}, Pending: []PendingMigration{}, Edited: []EditedMigration{}}
}

// serveSchemaStatus is GET /_portal/api/db/schema-status: the status
// computed now. An app without a database answers {"database": false}.
func (s *Server) serveSchemaStatus(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Database.SchemaStatus == nil {
		_ = writeJSON(w, http.StatusOK, noDatabaseSchema())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	st, err := s.cfg.Database.SchemaStatus(ctx)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "the schema status can't be computed: "+err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, st)
}

// publishSchema computes the status and publishes it as a schema event
// with source; the SQL editor uses it after a committed DDL statement.
func (s *Server) publishSchema(ctx context.Context, source string) {
	if s.cfg.Database.SchemaStatus == nil {
		return
	}
	st, err := s.cfg.Database.SchemaStatus(ctx)
	if err != nil {
		s.cfg.Logf("orb: dev portal couldn't compute the schema status: %v", err)
		return
	}
	st.Source, st.Applied = source, []string{}
	s.cfg.Hub.SetSchema(st)
}
