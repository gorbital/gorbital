package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

// The Dev Portal's generators hub runs orb add through plans (ADR-0077):
// what each command would write, as a diff, then the write. add-mail and
// add-storage rewrite files the command computes; add-rls writes its
// migration, manifest and lock; add-orgs works on a git branch with
// builds and commits, so its plan is the dry run's summary and its
// apply runs the command.

// addMailInputJSON is orb add mail's answers as the portal sends them.
type addMailInputJSON struct {
	Provider     string `json:"provider"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     string `json:"smtp_port"`
	SMTPTLS      string `json:"smtp_tls"`
	SMTPUsername string `json:"smtp_username"`
}

// planAddMail plans orb add mail for the portal.
func planAddMail(ctx context.Context, app appInfo, input json.RawMessage) (genplan.Plan, mailPlan, error) {
	var raw addMailInputJSON
	if err := decodeInput(input, &raw); err != nil {
		return genplan.Plan{}, mailPlan{}, err
	}
	in := mailInput{provider: raw.Provider, smtpHost: raw.SMTPHost, smtpPort: raw.SMTPPort, smtpTLS: raw.SMTPTLS, smtpUsername: raw.SMTPUsername}
	if in.provider == "" {
		in.provider = recipes.MailResend
		if in.smtpHost != "" {
			in.provider = recipes.MailSMTP
		}
	}
	if in.provider != recipes.MailResend && in.provider != recipes.MailSMTP {
		return genplan.Plan{}, mailPlan{}, usageError(fmt.Sprintf("provider must be resend or smtp, got %q", in.provider))
	}
	if err := normalizeMail(&in); err != nil {
		return genplan.Plan{}, mailPlan{}, err
	}
	if _, err := mailLayout(app.dir); err != nil {
		return genplan.Plan{}, mailPlan{}, usageError("orb add mail works in apps created with the Full preset")
	}
	example, err := os.ReadFile(filepath.Join(app.dir, envExamplePath))
	if err != nil {
		return genplan.Plan{}, mailPlan{}, fmt.Errorf("orb add mail needs %s: %w", envExamplePath, err)
	}
	goMod, err := readGoMod(ctx, app.dir)
	if err != nil {
		return genplan.Plan{}, mailPlan{}, err
	}
	recipe, err := recipes.RenderMail(in.provider, goMod.Module.Path)
	if err != nil {
		return genplan.Plan{}, mailPlan{}, err
	}
	mp, err := planMail(app.dir, example, recipe, goMod, in)
	if err != nil {
		return genplan.Plan{}, mailPlan{}, err
	}
	plan := genplan.Plan{Generator: "add-mail", Name: recipe.Label}
	for _, w := range mp.writes {
		plan.Changes = append(plan.Changes, changeFor(app.dir, w.path, w.content))
	}
	if len(mp.modules) > 0 {
		plan.Next = append(plan.Next, "go.mod gains "+strings.Join(mp.modules, ", ")+" (go mod edit), then go mod tidy runs")
	}
	if mp.empty() {
		plan.Summary = fmt.Sprintf("The app already sends email with %s; nothing to change.", recipe.Label)
	} else {
		plan.Summary = mailSummary(in, recipe, mp)
		plan.Next = append(plan.Next, strings.Split(strings.TrimSpace(mailNextSteps(app.dir, in, mp)), "\n")...)
	}
	return plan, mp, nil
}

// applyAddMail writes the plan as orb add mail does (files, go.mod, tidy).
func applyAddMail(ctx context.Context, dir string, mp mailPlan, allowDirty bool, stderr func(string)) error {
	var out strings.Builder
	err := applyMail(ctx, dir, mp, allowDirty, false, &out)
	if out.Len() > 0 && stderr != nil {
		stderr(strings.TrimSpace(out.String()))
	}
	return err
}

// addStorageInputJSON is orb add storage's answers.
type addStorageInputJSON struct {
	Driver    string `json:"driver"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	PublicURL string `json:"public_url"`
}

// planAddStorage plans orb add storage for the portal.
func planAddStorage(app appInfo, input json.RawMessage) (genplan.Plan, storagePlan, error) {
	var raw addStorageInputJSON
	if err := decodeInput(input, &raw); err != nil {
		return genplan.Plan{}, storagePlan{}, err
	}
	in := storageInput{driver: raw.Driver, endpoint: raw.Endpoint, region: raw.Region, bucket: raw.Bucket, accessKey: raw.AccessKey, publicURL: raw.PublicURL}
	if in.driver == "" {
		in.driver = "local"
	}
	sp, err := planStorage(app.dir, in)
	if err != nil {
		return genplan.Plan{}, storagePlan{}, err
	}
	plan := genplan.Plan{Generator: "add-storage", Name: in.driver}
	for _, w := range sp.writes {
		plan.Changes = append(plan.Changes, changeFor(app.dir, w.path, w.content))
	}
	plan.Summary = fmt.Sprintf("File storage with %s: %s", in.driver, strings.Join(sp.paths(), ", "))
	plan.Next = storageNextSteps(in.driver)
	return plan, sp, nil
}

// addRLSPlan is what orb add rls writes.
type addRLSPlan struct {
	writes []fileWrite
}

// planAddRLS plans orb add rls for the portal: the policy migration, the
// manifest key and the lock.
func planAddRLS(ctx context.Context, app appInfo) (genplan.Plan, addRLSPlan, error) {
	lock, err := readLock(app.dir)
	switch {
	case errors.Is(err, errNoLock):
		return genplan.Plan{}, addRLSPlan{}, usageError("orb add rls works in apps created with orb new --preset full --tenancy multi")
	case err != nil:
		return genplan.Plan{}, addRLSPlan{}, err
	case lock.Inputs.Preset != "full" || lock.Inputs.Tenancy != recipes.TenancyMulti:
		return genplan.Plan{}, addRLSPlan{}, usageError("row-level security separates organisations' data, and this app is single-tenant; add organisations first (orb add orgs)")
	case lock.Inputs.RLS:
		return genplan.Plan{Generator: "add-rls", Name: "rls", Summary: "The app already has row-level security; nothing to change."}, addRLSPlan{}, nil
	case lock.Orb.Version != Version || (lock.Orb.Revision != "" && lock.Orb.Revision != buildRevision()):
		return genplan.Plan{}, addRLSPlan{}, usageError("the app was last written by orb " + cmpOr(lock.Orb.Version, "(unknown)") + "; run orb upgrade first")
	}
	if !insideGitRepo(ctx, app.dir) {
		return genplan.Plan{}, addRLSPlan{}, usageError("orb add rls needs the app in git, so the change can be reviewed")
	}
	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, addRLSPlan{}, err
	}
	defer root.Close()
	policy, err := root.ReadFile(recipes.RowLevelSecurityPath)
	if err != nil {
		return genplan.Plan{}, addRLSPlan{}, fmt.Errorf("the app has no %s; restore it from git history or run orb upgrade", recipes.RowLevelSecurityPath)
	}
	manifest, err := root.ReadFile(manifestPath)
	if err != nil {
		return genplan.Plan{}, addRLSPlan{}, err
	}
	version, err := nextMigrationVersion(app.dir, time.Now())
	if err != nil {
		return genplan.Plan{}, addRLSPlan{}, err
	}
	migration := "db/migrations/" + version + "_row_level_security.sql"
	manifest = recipes.SetManifestKey(manifest, recipes.RowLevelSecurityKey, "true")
	lock.Inputs.RLS = true
	lock.record(manifestPath, manifest)
	lockData, err := lock.encode()
	if err != nil {
		return genplan.Plan{}, addRLSPlan{}, err
	}
	rp := addRLSPlan{writes: []fileWrite{{path: migration, content: policy, perm: 0o644}, {path: manifestPath, content: manifest, perm: 0o644}, {path: lockPath, content: lockData, perm: 0o644}}}
	plan := genplan.Plan{Generator: "add-rls", Name: "rls", Summary: "Row-level security: the policy migration " + migration + ", the manifest key and the lock."}
	for _, w := range rp.writes {
		plan.Changes = append(plan.Changes, changeFor(app.dir, w.path, w.content))
	}
	plan.Next = []string{
		"Make sure DATABASE_URL connects as a role without superuser or BYPASSRLS (docs/guides/row-level-security.md)",
		migrateCommand(app.dir) + ", then orb doctor and go test ./...",
		"git add -A && git commit -m 'Add row-level security'",
	}
	return plan, rp, nil
}

// changeFor makes a create or modify change from the file on disk.
func changeFor(dir, path string, content []byte) genplan.Change {
	before, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		return genplan.Change{Path: path, Kind: genplan.Create, Content: content}
	}
	return genplan.Change{Path: path, Kind: genplan.Modify, Before: before, Content: content}
}

// planAddOrgs is orb add orgs' dry run: what it would do, since the
// command works on a git branch with builds and commits of its own.
func planAddOrgs(ctx context.Context, app appInfo) (genplan.Plan, error) {
	var out, errOut strings.Builder
	prev, _ := os.Getwd()
	if err := os.Chdir(app.dir); err != nil {
		return genplan.Plan{}, err
	}
	defer func() { _ = os.Chdir(prev) }()
	err := runAddOrgs(ctx, []string{"--dry-run", "--json"}, &out, &errOut)
	if err != nil {
		return genplan.Plan{}, fmt.Errorf("%w%s", err, strings.TrimSpace(errOut.String()))
	}
	var res struct {
		Branch   string `json:"branch"`
		UpToDate bool   `json:"up_to_date"`
		Changes  []struct {
			Path string `json:"path"`
		} `json:"changes"`
		Conflicts []string `json:"conflicts"`
	}
	_ = json.Unmarshal([]byte(out.String()), &res)
	plan := genplan.Plan{Generator: "add-orgs", Name: "orgs"}
	if res.UpToDate {
		plan.Summary = "The app already has organisations; nothing to change."
		return plan, nil
	}
	plan.Summary = fmt.Sprintf("Turns the app multi-tenant on branch %s: organisations with members, roles and invitations; %d files change. orb add orgs builds the app, regenerates api/, records api/surface.json and commits on that branch; review it there.", cmpOr(res.Branch, addOrgsBranch), len(res.Changes))
	if len(res.Conflicts) > 0 {
		plan.Summary += fmt.Sprintf(" %d files need a merge by hand: %s.", len(res.Conflicts), strings.Join(res.Conflicts, ", "))
	}
	for _, c := range res.Changes {
		plan.Changes = append(plan.Changes, genplan.Change{Path: c.Path, Kind: genplan.Modify})
	}
	plan.Next = []string{"Review the branch " + cmpOr(res.Branch, addOrgsBranch) + " and merge it", "docs/guides/organisations.md"}
	return plan, nil
}

// applyAddOrgs runs orb add orgs.
func applyAddOrgs(ctx context.Context, app appInfo, log func(string)) error {
	var out, errOut strings.Builder
	prev, _ := os.Getwd()
	if err := os.Chdir(app.dir); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(prev) }()
	err := runAddOrgs(ctx, nil, &out, &errOut)
	if log != nil {
		if s := strings.TrimSpace(out.String() + "\n" + errOut.String()); s != "" {
			log(s)
		}
	}
	return err
}
