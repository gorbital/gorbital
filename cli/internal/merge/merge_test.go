package merge

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

const (
	appV1     = "package app\n\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n"
	appV2     = "package app\n\nfunc a() {}\n\nfunc b() { secure() }\n\nfunc c() {}\n"
	appEdited = "package app\n\n// edited\nfunc a() {}\n\nfunc b() {}\n\nfunc c() {}\n"
	appClash  = "package app\n\nfunc a() {}\n\nfunc b() { mine() }\n\nfunc c() {}\n"
)

func plan(t *testing.T, base, theirs, ours map[string]string, unproven ...string) map[string]Change {
	t.Helper()
	in := Input{Base: bytesMap(base), Theirs: bytesMap(theirs), Unproven: map[string]bool{}, Label: "apistock v0.5.0"}
	for _, p := range unproven {
		in.Unproven[p] = true
	}
	in.Ours = func(p string) ([]byte, bool, error) {
		s, ok := ours[p]
		return []byte(s), ok, nil
	}
	changes, err := Plan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Change{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	return byPath
}

func bytesMap(m map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range m {
		out[k] = []byte(v)
	}
	return out
}

func TestPlan(t *testing.T) {
	const f = "internal/app/app.go"
	for _, tt := range []struct {
		name               string
		base, theirs, ours map[string]string
		unproven           []string
		want               Action
		content            string // "" skips the check
		contains           []string
	}{
		{name: "never edited takes the template", base: m(f, appV1), theirs: m(f, appV2), ours: m(f, appV1), want: Update, content: appV2},
		{name: "template unchanged keeps edits", base: m(f, appV1), theirs: m(f, appV1), ours: m(f, appEdited), want: Unchanged},
		{name: "already the same", base: m(f, appV1), theirs: m(f, appV2), ours: m(f, appV2), want: Unchanged},
		{name: "separate edits merge", base: m(f, appV1), theirs: m(f, appV2), ours: m(f, appEdited), want: Merged, contains: []string{"// edited", "secure()"}},
		{name: "same line conflicts", base: m(f, appV1), theirs: m(f, appV2), ours: m(f, appClash), want: Conflict, contains: []string{"<<<<<<< yours", "mine()", "secure()", ">>>>>>> apistock v0.5.0"}},
		{name: "unproven base never takes theirs silently", base: m(f, appV1), theirs: m(f, appV2), ours: m(f, appV1), unproven: []string{f}, want: Conflict, contains: []string{"secure()"}},
		{name: "new file", base: m(), theirs: m(f, appV1), ours: m(), want: Create, content: appV1},
		{name: "new file clashes with the developer's", base: m(), theirs: m(f, appV2), ours: m(f, appClash), want: Conflict, contains: []string{"mine()", "secure()"}},
		{name: "removed and never edited", base: m(f, appV1), theirs: m(), ours: m(f, appV1), want: Delete},
		{name: "removed but edited is kept", base: m(f, appV1), theirs: m(), ours: m(f, appEdited), want: Kept},
		{name: "removed and already deleted", base: m(f, appV1), theirs: m(), ours: m(), want: Unchanged},
		{name: "deleted by the developer, template same", base: m(f, appV1), theirs: m(f, appV1), ours: m(), want: Unchanged},
		{name: "deleted by the developer, template changed", base: m(f, appV1), theirs: m(f, appV2), ours: m(), want: Kept},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := plan(t, tt.base, tt.theirs, tt.ours, tt.unproven...)[f]
			if c.Action != tt.want {
				t.Fatalf("action = %s, want %s (content %q)", c.Action, tt.want, c.Content)
			}
			if tt.content != "" && string(c.Content) != tt.content {
				t.Errorf("content = %q, want %q", c.Content, tt.content)
			}
			for _, s := range tt.contains {
				if !bytes.Contains(c.Content, []byte(s)) {
					t.Errorf("content lacks %q:\n%s", s, c.Content)
				}
			}
			// Every line the developer added or changed survives a merge or
			// conflict; lines they never touched may take the template's change.
			if c.Action == Merged || c.Action == Conflict {
				for line := range strings.Lines(tt.ours[f]) {
					if strings.Contains(tt.base[f], line) {
						continue
					}
					if !bytes.Contains(c.Content, []byte(line)) {
						t.Errorf("developer line %q lost:\n%s", line, c.Content)
					}
				}
			}
		})
	}
}

func TestPlanMigrations(t *testing.T) {
	const released, added = "db/migrations/20260915000002_projects.sql", "db/migrations/20260920000001_retention.sql"
	changes := plan(t,
		m(released, "CREATE TABLE projects ();\n", "db/migrations/20260914000001_settings.sql", "-- settings\n"),
		m(released, "CREATE TABLE projects ();\n", added, "-- retention\n"),
		m(released, "CREATE TABLE projects (); -- edited\n", "db/migrations/20260914000001_settings.sql", "-- settings\n"),
	)
	if c := changes[added]; c.Action != Create {
		t.Errorf("new migration = %s, want create", c.Action)
	}
	if c := changes["db/migrations/20260914000001_settings.sql"]; c.Action != Unchanged {
		t.Errorf("migration removed from the release = %s, want unchanged: the database already ran it", c.Action)
	}
	if c := changes[released]; c.Action != Unchanged {
		t.Errorf("edited released migration = %s, want unchanged", c.Action)
	}

	in := Input{
		Base:   bytesMap(m(released, "CREATE TABLE projects ();\n")),
		Theirs: bytesMap(m(released, "CREATE TABLE projects (id text);\n")),
		Ours:   func(string) ([]byte, bool, error) { return []byte("CREATE TABLE projects ();\n"), true, nil },
		Label:  "apistock v0.5.0",
	}
	if _, err := Plan(context.Background(), in); err == nil {
		t.Error("Plan accepted a release that changes a released migration")
	}
}

func m(kv ...string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}
