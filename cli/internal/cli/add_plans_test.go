package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/genplan"
)

func TestAddStoragePlan(t *testing.T) {
	dir := newStorageApp(t)
	app := appInfo{dir: dir, module: "example.com/shop"}
	plan, sp, err := planAddStorage(app, json.RawMessage(`{"driver":"minio"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Generator != "add-storage" || plan.Name != "minio" || len(plan.Changes) != 3 || len(sp.writes) != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	kinds := map[string]genplan.Kind{}
	for _, c := range plan.Changes {
		kinds[c.Path] = c.Kind
		if c.Kind == genplan.Modify && len(c.Before) == 0 {
			t.Errorf("%s: a modify without its before", c.Path)
		}
	}
	if kinds[".env.example"] != genplan.Modify || kinds[".env"] != genplan.Modify || kinds["compose.yaml"] != genplan.Modify {
		t.Errorf("kinds = %v", kinds)
	}
	if !strings.Contains(string(plan.Changes[0].Content), "STORAGE_DRIVER=minio") {
		t.Errorf(".env.example content = %s", plan.Changes[0].Content)
	}
	if _, _, err := planAddStorage(app, json.RawMessage(`{"driver":"gcs"}`)); err == nil {
		t.Error("a bad driver was planned")
	}
	if _, _, err := planAddStorage(app, json.RawMessage(`{"nope":1}`)); err == nil {
		t.Error("an unknown field was accepted")
	}
	if err := applyStorage(dir, sp); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, ".env")); !strings.Contains(got, "STORAGE_DRIVER=minio") {
		t.Errorf(".env after apply = %s", got)
	}
}

func TestAddMailPlan(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n\nrequire gorbital.dev v0.0.0\n")
	writeFile(t, filepath.Join(dir, "internal", "app", "mail.go"), "package app\n")
	writeFile(t, filepath.Join(dir, ".env.example"), "APP_ADDR=127.0.0.1:8080\n\n# orb:begin mail\nRESEND_API_KEY=\n# orb:end mail\n")
	writeFile(t, filepath.Join(dir, ".env"), "APP_ADDR=127.0.0.1:8080\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), ".env\n")
	app := appInfo{dir: dir, module: "example.com/shop"}
	plan, mp, err := planAddMail(context.Background(), app, json.RawMessage(`{"provider":"smtp","smtp_host":"smtp.example.com","smtp_username":"ada"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Generator != "add-mail" || mp.empty() || len(plan.Changes) == 0 || plan.Summary == "" {
		t.Fatalf("plan = %+v", plan)
	}
	var paths []string
	for _, c := range plan.Changes {
		paths = append(paths, c.Path)
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "internal/app/infra_mail.go") || !strings.Contains(joined, ".env.example") {
		t.Errorf("paths = %s", joined)
	}
	if _, _, err := planAddMail(context.Background(), app, json.RawMessage(`{"provider":"pigeon"}`)); err == nil {
		t.Error("a bad provider was planned")
	}
}

func TestNewNoStart(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	code, out, errOut := runOrb(t, "new", "shop", "--preset", "minimal", "--skip-tidy", "--no-git", "--no-start", "--yes")
	if code != 0 || strings.Contains(out, "starting orb dev") {
		t.Fatalf("orb new --no-start = %d %q %q", code, out, errOut)
	}
	if !strings.Contains(out, "orb dev") {
		t.Errorf("the next steps don't mention orb dev:\n%s", out)
	}
}
