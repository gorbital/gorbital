package archtest

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// multiTenantChanges are the paths examples/full-multi may change or add
// compared with examples/full-single, the golden apps on gorbital.Main
// (ADR-0048, ADR-0083). A path ending in / covers a directory. Every other
// file must be identical, so the two golden apps can't drift apart: fix a
// shared file in both.
var multiTenantChanges = []string{
	// main.go adds the organisations module.
	"cmd/api/main.go",
	"cmd/api/app_test.go", // tests the app with organisations
	// Projects belong to an organisation instead of a user.
	// Since v0.3.0 orb new generates the module rather than writing it
	// from templates (ADR-0090 §5), so both apps' migrations have the
	// one version every app of a release gets, and only their contents
	// differ.
	"db/migrations/20260918000071_projects.sql",
	"internal/modules/projects/",
	// Organisations, the app's own code as orb new copies it (v0.2.1), and
	// its migrations.
	"internal/modules/orgs/",
	"db/migrations/20260916000001_orgs.sql",
	"db/migrations/20260918000002_settings_org_purge.sql",
	// Row-level security (ADR-0061): the policies orb add rls turns into a
	// migration.
	"db/row_level_security.sql",

	"go.mod",
	"go.sum", // organisations pull in what the single-tenant app doesn't
	"gorbital.yaml",
	// Generated or written for each app.
	"api/openapi.json",
	"api/postman_collection.json",
	"api/llms.txt",
	"api/openapi.baseline.json",
	"api/surface.json",
	"README.md",
	"ARCHITECTURE.md",
	"AGENTS.md",
}

// v01MultiTenantChanges are the paths examples/v0.1/full-multi may change or
// add compared with examples/v0.1/full-single, the frozen v0.1-layout golden
// apps.
var v01MultiTenantChanges = []string{
	// Organisations: the module, its job, wiring, migration and tests.
	"internal/modules/orgs/",
	"internal/jobs/orgspurge/",
	"db/migrations/20260916000001_orgs.sql",
	"internal/app/job_orgs_purge.go",
	"internal/app/module_orgs.go",
	"internal/app/orgs_hooks.go",
	"internal/app/orgs_test.go",
	// Organisations' own settings (ADR-0056): purged with the organisation.
	"db/migrations/20260918000002_settings_org_purge.sql",
	"internal/app/org_settings_test.go",
	"internal/app/orgs_service_accounts.go",      // organisation service accounts (ADR-0058)
	"internal/app/orgs_service_accounts_test.go", // and their cross-organisation denial tests
	// Feature flags as the organisation (ADR-0057).
	"internal/app/org_flags_test.go",
	// Row-level security (ADR-0061): the policies orb add rls turns into a
	// migration, and the tests that run the app as a role without bypass.
	"db/row_level_security.sql",
	"internal/app/rls_test.go",

	// Wiring that names the orgs module.
	"go.mod",
	"gorbital.yaml",
	"internal/app/app.go",
	"internal/app/commands.go",
	"internal/app/jobs.go",
	"internal/app/modules.go",
	"internal/app/permissions.go",
	"internal/app/settings.go",
	"internal/app/seed.go",
	"internal/app/seed_test.go",
	"internal/app/app_test.go",      // personalWorkspace, shared by org-scoped resource tests
	"internal/app/mail_previews.go", // the organisation invitation preview (ADR-0078)

	// Projects belong to an organisation instead of a user.
	"db/migrations/20260915000002_projects.sql",
	"db/migrations/20260916000002_projects.sql",
	"internal/modules/projects/",
	"internal/app/module_projects.go",
	"internal/app/projects_test.go",

	// Generated or written for each app.
	"api/openapi.json",
	"api/postman_collection.json",
	"api/llms.txt",
	"api/openapi.baseline.json",
	"api/surface.json",
	"README.md",
	"ARCHITECTURE.md",
	"AGENTS.md",
}

func allowedChange(changes []string, path string) bool {
	return slices.ContainsFunc(changes, func(p string) bool {
		return path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p))
	})
}

// appFiles returns the files of the golden app in dir, by slash path.
func appFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.Base(path), ".env") && filepath.Base(path) != ".env.example" {
			return nil // local secrets, never compared
		}
		data, err := os.ReadFile(path)
		files[filepath.ToSlash(rel)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestGoldenAppsDontDrift checks that examples/full-multi is
// examples/full-single plus organisations and nothing else, and the same of
// the v0.1-layout golden apps in examples/v0.1.
func TestGoldenAppsDontDrift(t *testing.T) {
	for _, pair := range []struct {
		dir     string
		changes []string
	}{
		{filepath.Join("..", "..", "examples"), multiTenantChanges},
		{filepath.Join("..", "..", "examples", "v0.1"), v01MultiTenantChanges},
	} {
		t.Run(pair.dir, func(t *testing.T) { checkDrift(t, pair.dir, pair.changes) })
	}
}

func checkDrift(t *testing.T, root string, changes []string) {
	single, multi := appFiles(t, filepath.Join(root, "full-single")), appFiles(t, filepath.Join(root, "full-multi"))

	paths := make([]string, 0, len(single)+len(multi))
	for p := range single {
		paths = append(paths, p)
	}
	for p := range multi {
		if _, ok := single[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)

	for _, p := range paths {
		a, inSingle := single[p]
		b, inMulti := multi[p]
		if allowedChange(changes, p) {
			continue
		}
		switch {
		case !inMulti:
			t.Errorf("%s is in full-single but not full-multi; add it to both, or to the allowed changes if organisations remove it", p)
		case !inSingle:
			t.Errorf("%s is only in full-multi; add it to full-single too, or to the allowed changes if only organisations need it", p)
		case !bytes.Equal(a, b):
			t.Errorf("%s differs between full-single and full-multi; make the same change in both, or add it to the allowed changes", p)
		}
	}

	// An allowed file that no longer differs should come off the list.
	for _, p := range changes {
		if strings.HasSuffix(p, "/") {
			continue
		}
		if a, ok := single[p]; ok {
			if b, ok := multi[p]; ok && bytes.Equal(a, b) {
				t.Errorf("%s is in the allowed changes but is identical in both apps; remove it from the list", p)
			}
		}
	}
}
