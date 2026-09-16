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
// compared with examples/full-single (ADR-0048). A path ending in / covers a
// directory. Every other file must be identical, so the two golden apps
// can't drift apart: fix a shared file in both.
var multiTenantChanges = []string{
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
	"internal/app/app_test.go", // personalWorkspace, shared by org-scoped resource tests

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

func allowedChange(path string) bool {
	return slices.ContainsFunc(multiTenantChanges, func(p string) bool {
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
// examples/full-single plus organisations and nothing else.
func TestGoldenAppsDontDrift(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
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
		if allowedChange(p) {
			continue
		}
		switch {
		case !inMulti:
			t.Errorf("%s is in full-single but not full-multi; add it to both, or to multiTenantChanges if organisations remove it", p)
		case !inSingle:
			t.Errorf("%s is only in full-multi; add it to full-single too, or to multiTenantChanges if only organisations need it", p)
		case !bytes.Equal(a, b):
			t.Errorf("%s differs between full-single and full-multi; make the same change in both, or add it to multiTenantChanges", p)
		}
	}

	// An allowed file that no longer differs should come off the list.
	for _, p := range multiTenantChanges {
		if strings.HasSuffix(p, "/") {
			continue
		}
		if a, ok := single[p]; ok {
			if b, ok := multi[p]; ok && bytes.Equal(a, b) {
				t.Errorf("%s is in multiTenantChanges but is identical in both apps; remove it from the list", p)
			}
		}
	}
}
