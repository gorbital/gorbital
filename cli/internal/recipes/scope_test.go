package recipes

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The golden output of the four access scopes (ADR-0091). Each renders the
// same record under one scope into testdata/scopes/<scope>, so a change to
// what --scope generates shows up as a reviewable diff:
//
//	go test ./internal/recipes -run TestScopeGolden -update
//
// The tenant scope's output under the organisations vocabulary is checked
// against the golden apps instead (TestModuleMatchesGoldenApps), which is
// what keeps a v0.2 app's module byte for byte what it was.
const scopeGoldenDir = "testdata/scopes"

// scopeGoldenFields are the fields every golden module has: a unique
// string, optional text and a choice, so the unique index, the CHECK
// constraints and the enum filter all appear.
var scopeGoldenFields = []string{"title:string:unique", "note:text", "state:enum(open,done)"}

// scopeGoldenData is the module the golden trees are rendered from.
func scopeGoldenData(t *testing.T, scope string, v Vocabulary) ModuleData {
	t.Helper()
	fields, err := ParseModuleFields(scopeGoldenFields)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewModuleData("example.com/app", "Record", fields, ResourceOptions{
		Scope: scope, Vocabulary: v, Migration: "20260101000000",
	})
	if err != nil {
		t.Fatalf("%s: %v", scope, err)
	}
	return d
}

// merchantVocabulary is an app that calls its tenant a merchant, as
// gorbital.yaml's scope block records it (ADR-0088).
func merchantVocabulary() Vocabulary {
	return Vocabulary{Name: "merchant", Roles: []string{"owner", "manager", "courier"}, Declared: true}.withDefaults()
}

func TestScopeGolden(t *testing.T) {
	for _, tt := range []struct {
		dir        string
		scope      string
		vocabulary Vocabulary
	}{
		{"user", ScopeUser, Vocabulary{}},
		{"tenant", ScopeTenant, Vocabulary{}},
		{"tenant-merchant", ScopeTenant, merchantVocabulary()},
		{"public", ScopePublic, Vocabulary{}},
		{"custom", ScopeCustom, Vocabulary{}},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			d := scopeGoldenData(t, tt.scope, tt.vocabulary)
			files, err := RenderModule(d)
			if err != nil {
				t.Fatalf("RenderModule() error = %v", err)
			}
			golden := filepath.Join(scopeGoldenDir, tt.dir)
			written := map[string]bool{}
			for _, f := range files {
				written[f.Path] = true
				target := filepath.Join(golden, filepath.FromSlash(f.Path))
				if *updateGolden {
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, f.Content, 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(target)
				if err != nil {
					t.Errorf("read golden %s: %v", f.Path, err)
					continue
				}
				if !bytes.Equal(f.Content, want) {
					t.Errorf("%s differs from %s (run with -update and review the diff)", f.Path, target)
				}
			}
			if *updateGolden {
				return
			}
			// A golden file the generator no longer writes is a file the
			// tree has and the scope doesn't.
			err = filepath.WalkDir(golden, func(path string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(golden, path)
				if !written[filepath.ToSlash(rel)] {
					t.Errorf("%s isn't written by --scope %s any more", path, tt.scope)
				}
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
}

// TestOrgIsTenant checks that the deprecated --scope org can't drift from
// --scope tenant: it is the same scope under its old name, so it generates
// the same bytes in the same app, whatever the app calls its tenant.
func TestOrgIsTenant(t *testing.T) {
	for _, v := range []Vocabulary{{}, merchantVocabulary()} {
		org, tenant := scopeGoldenData(t, ScopeOrg, v), scopeGoldenData(t, ScopeTenant, v)
		if org.Scope != ScopeTenant {
			t.Fatalf("--scope org resolved to %q, want %q", org.Scope, ScopeTenant)
		}
		orgFiles, err := RenderModule(org)
		if err != nil {
			t.Fatal(err)
		}
		tenantFiles, err := RenderModule(tenant)
		if err != nil {
			t.Fatal(err)
		}
		if len(orgFiles) != len(tenantFiles) {
			t.Fatalf("--scope org wrote %d files, --scope tenant %d", len(orgFiles), len(tenantFiles))
		}
		for i, f := range orgFiles {
			if f.Path != tenantFiles[i].Path || !bytes.Equal(f.Content, tenantFiles[i].Content) {
				t.Errorf("%s: --scope org differs from --scope tenant in a %s app", f.Path, v.Name)
			}
		}
	}
}

// TestCustomShipsARedTest checks what --scope custom promises: the module
// arrives with a test that fails, because gorbital doesn't know who may
// see one record and a module that serves every row must not look
// finished. The test's own failure message names the file to write.
func TestCustomShipsARedTest(t *testing.T) {
	d := scopeGoldenData(t, ScopeCustom, Vocabulary{})
	files, err := RenderModule(d)
	if err != nil {
		t.Fatal(err)
	}
	policy, test := moduleFile(t, files, d.Dir()+"/policy.go"), moduleFile(t, files, d.Dir()+"/policy_test.go")

	// The three stubs return the error, so the test that looks for it fails.
	for _, method := range []string{"func (p Policy) CanRead(", "func (p Policy) CanWrite(", "func (p Policy) Filter("} {
		decl := policy[strings.Index(policy, method):]
		if body, _, _ := strings.Cut(decl, "\n}"); !strings.Contains(body, "return gorbital.ErrNotImplemented") {
			t.Errorf("policy.go's %s doesn't return gorbital.ErrNotImplemented:\n%s", method, body)
		}
	}
	for _, want := range []string{"TestPolicyIsImplemented", "TestFilterNarrowsTheList", "gorbital.ErrNotImplemented", "t.Errorf("} {
		if !strings.Contains(test, want) {
			t.Errorf("policy_test.go has no %s", want)
		}
	}

	// No tenant filter and no tenant column: with custom, isolation is the
	// module's own (ADR-0091 §5).
	migration := moduleFile(t, files, d.MigrationPath())
	for _, forbidden := range []string{"org_id", "owner_id"} {
		if strings.Contains(migration, forbidden) {
			t.Errorf("the migration has a %s column, which --scope custom must not add:\n%s", forbidden, migration)
		}
	}
	for _, f := range files {
		if strings.Contains(f.Path, "/repository/") && bytes.Contains(f.Content, []byte("org_id = $")) {
			t.Errorf("%s filters by a tenant column the developer didn't ask for", f.Path)
		}
	}

	// The repository asks the policy before every list, and the use cases
	// before they return or change a record.
	list := moduleFile(t, files, d.Dir()+"/repository/select_"+d.Table+".go")
	if !strings.Contains(list, "s.filter.Filter(ctx,") {
		t.Errorf("the list query doesn't ask the policy which rows it may return:\n%s", list)
	}
	for path, call := range map[string]string{
		"/usecase/get_":    "s.policy.CanRead(ctx,",
		"/usecase/create_": "s.policy.CanWrite(ctx,",
		"/usecase/update_": "s.policy.CanWrite(ctx,",
		"/usecase/delete_": "s.policy.CanWrite(ctx,",
	} {
		i := slices.IndexFunc(files, func(f JobFile) bool { return strings.Contains(f.Path, path) })
		if i < 0 {
			t.Fatalf("no %s file", path)
		}
		if !bytes.Contains(files[i].Content, []byte(call)) {
			t.Errorf("%s doesn't call %s", files[i].Path, call)
		}
	}
}

// TestPublicReadsAreOpenAndWritesAreNot checks the two halves of --scope
// public: guard.Public() on the reads, the write permission on the writes,
// no ownership column, and the comment the next reader of the module needs.
func TestPublicReadsAreOpenAndWritesAreNot(t *testing.T) {
	d := scopeGoldenData(t, ScopePublic, Vocabulary{})
	files, err := RenderModule(d)
	if err != nil {
		t.Fatal(err)
	}
	routes := moduleFile(t, files, d.Dir()+"/delivery/routes.go")
	if strings.Count(routes, "guard.Public()") != 2 {
		t.Errorf("the routes don't make both reads public:\n%s", routes)
	}
	for _, write := range []string{"h.createRecord", "h.updateRecord", "h.deleteRecord"} {
		call := routes[strings.Index(routes, write):]
		if statement, _, _ := strings.Cut(call, "\n\tgorbital."); strings.Contains(statement, "guard.Public()") {
			t.Errorf("%s is public; a public module's writes are permission-guarded", write)
		}
	}
	if !strings.Contains(routes, "world-readable") {
		t.Errorf("the routes don't say the records are world-readable:\n%s", routes)
	}
	migration := moduleFile(t, files, d.MigrationPath())
	for _, forbidden := range []string{"owner_id", "org_id", "created_by"} {
		if strings.Contains(migration, forbidden) {
			t.Errorf("a public record has no %s column:\n%s", forbidden, migration)
		}
	}
}

// moduleFile returns the rendered file at path, or fails.
func moduleFile(t *testing.T, files []JobFile, path string) string {
	t.Helper()
	i := slices.IndexFunc(files, func(f JobFile) bool { return f.Path == path })
	if i < 0 {
		t.Fatalf("no %s was generated", path)
	}
	return string(files[i].Content)
}

// TestReadVocabulary checks that the scope block is read defensively: it
// is written by a later release than the one reading it, so an app
// without it, and a block with keys this release doesn't know, both work.
func TestReadVocabulary(t *testing.T) {
	for _, tt := range []struct {
		name     string
		manifest string
		want     Vocabulary
	}{
		{"no manifest at all", "", OrganisationVocabulary()},
		{"a v0.2 manifest", "name: acme\ntenancy: multi\nfeatures: [postgres, orgs]\n", OrganisationVocabulary()},
		{"the scope block", "tenancy: multi\nscope:\n  name: merchant\n  roles: [owner, manager, courier]\n", merchantVocabulary()},
		{"keys this release doesn't know", "scope:\n  name: merchant\n  roles: [owner, manager, courier]\n  session: postgres.WithScope\n  resolver: subdomain\n", merchantVocabulary()},
		{"quoted values and comments", "scope:\n  # what a tenant is\n  name: \"merchant\"\n  roles: ['owner', 'manager', 'courier']\n", merchantVocabulary()},
		{"a malformed name", "scope:\n  name: not a name\n", OrganisationVocabulary()},
		{"an empty block", "scope:\n", OrganisationVocabulary()},
		{"a scope key that isn't a block", "scope: merchant\n", OrganisationVocabulary()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadVocabulary([]byte(tt.manifest))
			if got.Name != tt.want.Name || got.Column != tt.want.Column || got.PathParam != tt.want.PathParam ||
				got.NotFoundCode != tt.want.NotFoundCode || got.Segment != tt.want.Segment ||
				got.Declared != tt.want.Declared || !slices.Equal(got.Roles, tt.want.Roles) {
				t.Errorf("ReadVocabulary() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestParseScope checks the one validator every --scope goes through.
func TestParseScope(t *testing.T) {
	for in, want := range map[string]string{
		"":        "",
		"user":    ScopeUser,
		"tenant":  ScopeTenant,
		"org":     ScopeTenant, // the deprecated alias, accepted silently
		"public":  ScopePublic,
		"custom":  ScopeCustom,
		"Tenant":  "",
		"orgs":    "",
		"global":  "",
		"user ":   "",
		"tenant ": "",
	} {
		got, err := ParseScope(in)
		switch {
		case want == "" && in != "" && err == nil:
			t.Errorf("ParseScope(%q) = %q, want an error", in, got)
		case (want != "" || in == "") && (err != nil || got != want):
			t.Errorf("ParseScope(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// TestSetModuleScope checks the record orb routes and orb doctor read back.
func TestSetModuleScope(t *testing.T) {
	manifest := "name: acme\ntenancy: multi\n"
	manifest = string(SetModuleScope([]byte(manifest), "projects", ScopeTenant))
	manifest = string(SetModuleScope([]byte(manifest), "catalogue", ScopePublic))
	manifest = string(SetModuleScope([]byte(manifest), "tickets", ScopeCustom))
	manifest = string(SetModuleScope([]byte(manifest), "projects", ScopeUser)) // changed, not added twice

	if want := "modules:\n  catalogue: public\n  projects: user\n  tickets: custom\n"; !strings.HasSuffix(manifest, want) {
		t.Errorf("manifest =\n%s\nwant it ending with\n%s", manifest, want)
	}
	got := ReadModuleScopes([]byte(manifest))
	for module, scope := range map[string]string{"projects": ScopeUser, "catalogue": ScopePublic, "tickets": ScopeCustom} {
		if got[module] != scope {
			t.Errorf("ReadModuleScopes()[%q] = %q, want %q", module, got[module], scope)
		}
	}
	if len(got) != 3 {
		t.Errorf("ReadModuleScopes() = %v, want three modules", got)
	}
	// A manifest with no block, and one whose entries this release can't
	// read, are no scopes rather than an error.
	if got := ReadModuleScopes([]byte("name: acme\n")); len(got) != 0 {
		t.Errorf("ReadModuleScopes() of a manifest without the block = %v", got)
	}
	if got := ReadModuleScopes([]byte("modules:\n  projects: sideways\n  : broken\n")); len(got) != 0 {
		t.Errorf("ReadModuleScopes() of unreadable entries = %v", got)
	}
}
