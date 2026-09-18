package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/recipes"
)

const addUsage = `Usage: orb add mail [flags]
       orb add orgs [flags]
       orb add rls [flags]
       orb add storage [flags]

orb add mail sets up email in an app created with the Full preset: Resend or
any SMTP server. Run it again to switch provider.

orb add orgs turns a single-tenant Full app multi-tenant on a branch
(run "orb add orgs -h").

orb add rls turns on row-level security in a multi-tenant app
(run "orb add rls -h").

Secrets (the Resend API key, the SMTP password) go in .env, never in flags.
The sender name, address and reply-to are runtime settings, changed later in
/ops/settings without a redeploy.

In a terminal, missing values are asked interactively; pass flags to skip them.
`

func runAdd(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(stderr, addUsage)
		return usageError("missing feature: orb add mail, orb add orgs, orb add rls or orb add storage")
	}
	switch args[0] {
	case "mail":
		return runAddMail(ctx, args[1:], stdin, stdout, stderr)
	case "orgs":
		return runAddOrgs(ctx, args[1:], stdout, stderr)
	case "rls":
		return runAddRLS(ctx, args[1:], stdout, stderr)
	case "storage":
		return runAddStorage(ctx, args[1:], stdin, stdout, stderr)
	default:
		return usageError(fmt.Sprintf("unknown feature %q (want mail, orgs, rls or storage)", args[0]))
	}
}

const (
	envExamplePath = ".env.example"
	envPath        = ".env"
	manifestPath   = "gorbital.yaml"
)

// mailInput holds the user's email choices. Secrets come only from prompts.
type mailInput struct {
	provider     string
	apiKey       string
	smtpHost     string
	smtpPort     string
	smtpTLS      string
	smtpUsername string
	smtpPassword string
}

type addMailResult struct {
	Provider          string   `json:"provider"`
	AlreadyConfigured bool     `json:"already_configured"`
	Files             []string `json:"files"`
	EnvVariables      []string `json:"env_variables"`
	Modules           []string `json:"modules"`
	DryRun            bool     `json:"dry_run"`
}

func runAddMail(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb add mail", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var in mailInput
	flags.StringVar(&in.provider, "provider", "", "email provider: resend or smtp (default resend)")
	flags.StringVar(&in.smtpHost, "smtp-host", "", "SMTP server such as smtp.postmarkapp.com, saved in .env (implies --provider smtp)")
	flags.StringVar(&in.smtpPort, "smtp-port", "", "SMTP port such as 587 or 465, saved in .env (default 587)")
	flags.StringVar(&in.smtpTLS, "smtp-tls", "", "starttls, tls or none, saved in .env (default: tls for port 465, otherwise starttls)")
	flags.StringVar(&in.smtpUsername, "smtp-username", "", "SMTP username, saved in .env; put SMTP_PASSWORD in .env yourself or answer the prompt")
	dryRun := flags.Bool("dry-run", false, "show what would change without writing")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, addUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return usageError(fmt.Sprintf("unexpected argument %q: orb add mail takes only flags", args[0]))
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })

	smtpFlags := set["smtp-host"] || set["smtp-port"] || set["smtp-tls"] || set["smtp-username"]
	switch in.provider {
	case "":
		if smtpFlags {
			in.provider = recipes.MailSMTP
		}
	case recipes.MailResend:
		if smtpFlags {
			return usageError("--smtp-host, --smtp-port, --smtp-tls and --smtp-username apply only to --provider smtp")
		}
	case recipes.MailSMTP:
	default:
		return usageError(fmt.Sprintf("--provider must be resend or smtp, got %q", in.provider))
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	if _, err := mailLayout(app.dir); err != nil {
		return fmt.Errorf("%s %w", app.dir, err)
	}
	example, err := os.ReadFile(filepath.Join(app.dir, envExamplePath))
	if err != nil {
		return fmt.Errorf("orb add mail needs %s: %w", envExamplePath, err)
	}
	if _, err := recipes.Block(example, recipes.MailBlock); err != nil {
		return fmt.Errorf("%s has no email block; add these two lines where the provider's variables should go, then run orb add mail again:\n"+
			"  # orb:begin mail\n  # orb:end mail", envExamplePath)
	}

	ask := shouldPrompt(p, *asJSON, stdin, stdout)
	if ask {
		if err := promptMail(&in, set, p, stdin, stderr); err != nil {
			return err
		}
	}
	if in.provider == "" {
		in.provider = recipes.MailResend
	}
	if err := normalizeMail(&in); err != nil {
		return err
	}

	goMod, err := readGoMod(ctx, app.dir)
	if err != nil {
		return err
	}
	recipe, err := recipes.RenderMail(in.provider, goMod.Module.Path)
	if err != nil {
		return err
	}
	plan, err := planMail(app.dir, example, recipe, goMod, in)
	if err != nil {
		return err
	}
	result := addMailResult{
		Provider: in.provider, AlreadyConfigured: plan.empty(), Files: plan.paths(),
		EnvVariables: plan.envVars, Modules: plan.modules, DryRun: *dryRun,
	}
	appName := filepath.Base(app.dir)

	if plan.empty() {
		if *asJSON {
			return writeJSON(stdout, result)
		}
		fmt.Fprintf(stdout, "✓ %s already sends email with %s. Nothing to change.\n\n%s", appName, recipe.Label, mailNextSteps(app.dir, in, plan))
		return nil
	}

	summary := mailSummary(in, recipe, plan)
	if !*dryRun && ask {
		ok, err := confirm("Set up email with "+recipe.Label+"?", summary, p, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	}

	if !*dryRun {
		if err := applyMail(ctx, app.dir, plan, *allowDirty, *skipTidy, stderr); err != nil {
			return err
		}
	}

	if *asJSON {
		return writeJSON(stdout, result)
	}
	if *dryRun {
		fmt.Fprintf(stdout, "Would set up email with %s (dry run)\n\n%s\n", recipe.Label, summary)
		return nil
	}
	fmt.Fprintf(stdout, "✓ %s now sends email with %s\n\n%s\n\n%s", appName, recipe.Label, summary, mailNextSteps(app.dir, in, plan))
	return nil
}

// promptMail asks for the provider, then only that provider's questions.
func promptMail(in *mailInput, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	ask := func(fields ...huh.Field) error {
		if len(fields) == 0 {
			return nil
		}
		return runForm(huh.NewForm(huh.NewGroup(fields...)), p, stdin, stderr)
	}
	if in.provider == "" {
		in.provider = recipes.MailResend
		err := ask(huh.NewSelect[string]().Title("How should the app send email?").
			Description("Switch any time by running orb add mail again. In development, email always lands in Mailpit.").
			Options(
				huh.NewOption("Resend: an email API, the quickest to set up (recommended)", recipes.MailResend),
				huh.NewOption("SMTP: Amazon SES, Postmark, Mailgun, Google Workspace or your own server", recipes.MailSMTP),
			).Value(&in.provider))
		if err != nil {
			return err
		}
	}

	switch in.provider {
	case recipes.MailResend:
		return ask(
			huh.NewNote().Title("Setting up Resend").Description(
				"• Your API key goes in .env, which git ignores. It is never a flag or a runtime setting.\n"+
					"• The sender name, address and reply-to are set later in the admin API (/ops/settings), with no redeploy."),
			huh.NewInput().Title("Resend API key (optional)").
				Description("Paste a key from https://resend.com/api-keys to save it in .env now, or leave empty to add it later.").
				Placeholder("re_…").EchoMode(secretEchoMode(stdin)).
				Value(&in.apiKey).Validate(validateResendKey),
		)

	case recipes.MailSMTP:
		step := []huh.Field{huh.NewNote().Title("Setting up SMTP").Description(
			"• The server, port and credentials go in .env, which git ignores. The password is never a flag.\n" +
				"• The sender name, address and reply-to are set later in the admin API (/ops/settings), with no redeploy.")}
		if !set["smtp-host"] {
			step = append(step, huh.NewInput().Title("SMTP server (optional)").
				Description("Such as email-smtp.eu-west-1.amazonaws.com (Amazon SES), smtp.postmarkapp.com or smtp.gmail.com. Leave empty to add it to .env later.").
				Placeholder("smtp.example.com").Value(&in.smtpHost).Validate(validateSMTPHost))
		}
		if !set["smtp-port"] && !set["smtp-tls"] {
			choice := "587/starttls"
			step = append(step, huh.NewSelect[string]().Title("Port and encryption").Options(
				huh.NewOption("587 with STARTTLS (recommended)", "587/starttls"),
				huh.NewOption("465 with TLS", "465/tls"),
				huh.NewOption("2525 with STARTTLS", "2525/starttls"),
				huh.NewOption("25 without encryption (local servers only)", "25/none"),
			).Value(&choice))
			if err := ask(step...); err != nil {
				return err
			}
			in.smtpPort, in.smtpTLS, _ = strings.Cut(choice, "/")
		} else if err := ask(step...); err != nil {
			return err
		}
		if !set["smtp-username"] {
			if err := ask(huh.NewInput().Title("SMTP username (optional)").
				Description("Leave empty if the server needs no login.").
				Value(&in.smtpUsername).Validate(validateSingleWord)); err != nil {
				return err
			}
		}
		if in.smtpUsername != "" {
			return ask(huh.NewInput().Title("SMTP password (optional)").
				Description("Saved in .env only. Leave empty to add SMTP_PASSWORD to .env later.").
				EchoMode(secretEchoMode(stdin)).Value(&in.smtpPassword).Validate(validateSecret))
		}
	}
	return nil
}

// secretEchoMode hides typed secrets. Prompts only run on a terminal, whose
// stdin is a file; plain mode then reads the secret without echo. Scripted
// input (tests) has no terminal to hide it from.
func secretEchoMode(stdin io.Reader) huh.EchoMode {
	if _, ok := stdin.(interface{ Fd() uintptr }); ok {
		return huh.EchoModePassword
	}
	return huh.EchoModeNormal
}

// normalizeMail validates the input and fills SMTP defaults.
func normalizeMail(in *mailInput) error {
	in.apiKey = strings.TrimSpace(in.apiKey)
	if err := validateResendKey(in.apiKey); err != nil {
		return usageError("Resend API key " + err.Error())
	}
	if in.provider != recipes.MailSMTP {
		return nil
	}
	in.smtpHost = strings.TrimSpace(in.smtpHost)
	if err := validateSMTPHost(in.smtpHost); err != nil {
		return usageError("--smtp-host " + err.Error())
	}
	if in.smtpPort == "" {
		in.smtpPort = "587"
	}
	if port, err := strconv.Atoi(in.smtpPort); err != nil || port < 1 || port > 65535 {
		return usageError(fmt.Sprintf("--smtp-port must be a port number such as 587, got %q", in.smtpPort))
	}
	if in.smtpTLS == "" {
		in.smtpTLS = "starttls"
		if in.smtpPort == "465" {
			in.smtpTLS = "tls"
		}
	}
	in.smtpTLS = strings.ToLower(in.smtpTLS)
	if !slices.Contains([]string{"starttls", "tls", "none"}, in.smtpTLS) {
		return usageError(fmt.Sprintf("--smtp-tls must be starttls, tls or none, got %q", in.smtpTLS))
	}
	if err := validateSingleWord(in.smtpUsername); err != nil {
		return usageError("--smtp-username " + err.Error())
	}
	if err := validateSecret(in.smtpPassword); err != nil {
		return usageError("SMTP password " + err.Error())
	}
	return nil
}

// envValues are the .env variables to set. SMTP port and encryption are
// saved together with any server detail, so .env stays consistent.
func (in mailInput) envValues() map[string]string {
	values := map[string]string{}
	switch in.provider {
	case recipes.MailResend:
		if in.apiKey != "" {
			values["RESEND_API_KEY"] = in.apiKey
		}
	case recipes.MailSMTP:
		if in.smtpHost == "" && in.smtpUsername == "" && in.smtpPassword == "" {
			return values
		}
		values["SMTP_PORT"], values["SMTP_TLS"] = in.smtpPort, in.smtpTLS
		for key, v := range map[string]string{"SMTP_HOST": in.smtpHost, "SMTP_USERNAME": in.smtpUsername, "SMTP_PASSWORD": in.smtpPassword} {
			if v != "" {
				values[key] = v
			}
		}
	}
	return values
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func validateSMTPHost(s string) error {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return nil
	case strings.Contains(s, "://"):
		return errors.New("must be a host name without a scheme, such as smtp.example.com")
	case net.ParseIP(s) != nil:
		return nil
	case len(s) > 253 || !hostPattern.MatchString(s):
		return errors.New("must be a host name such as smtp.example.com")
	}
	return nil
}

func validateResendKey(s string) error {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return nil
	case !strings.HasPrefix(s, "re_"):
		return errors.New(`must start with "re_"; copy it from https://resend.com/api-keys`)
	case strings.ContainsAny(s, " \t\r\n\"'"):
		return errors.New("must not contain spaces or quotes")
	}
	return nil
}

func validateSingleWord(s string) error {
	if strings.ContainsAny(s, " \t\r\n\"'") {
		return errors.New("must not contain spaces or quotes")
	}
	return nil
}

func validateSecret(s string) error {
	if strings.ContainsAny(s, "\r\n") {
		return errors.New("must be a single line")
	}
	return nil
}

// mailPlan is every change orb add mail would make.
type mailPlan struct {
	writes     []fileWrite
	envVars    []string // variable names saved to .env, never values
	createsEnv bool
	modules    []string // gorbital modules to add to go.mod
	goMod      goModInfo
}

type fileWrite struct {
	path    string
	content []byte
	perm    fs.FileMode
}

func (p mailPlan) empty() bool { return len(p.writes) == 0 && len(p.modules) == 0 }

func (p mailPlan) paths() []string {
	paths := make([]string, 0, len(p.writes)+1)
	for _, w := range p.writes {
		paths = append(paths, w.path)
	}
	if len(p.modules) > 0 {
		paths = append(paths, "go.mod")
	}
	return paths
}

func (p mailPlan) writesEnv() bool {
	return slices.ContainsFunc(p.writes, func(w fileWrite) bool { return w.path == envPath })
}

// mailLayout returns the layout of the app in dir as orb add mail sees it:
// the v0.1 layout's internal/app/mail.go, or the v0.2 layout's
// cmd/api/mail.go.
func mailLayout(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "internal", "app", "mail.go")); err == nil {
		return recipes.LayoutV01, nil
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(recipes.MainMailPath))); err == nil {
		return recipes.LayoutV02, nil
	}
	return "", fmt.Errorf("has neither internal/app/mail.go nor %s: orb add mail works in apps created with the Full preset", recipes.MainMailPath)
}

func planMail(dir string, example []byte, r recipes.MailRecipe, goMod goModInfo, in mailInput) (mailPlan, error) {
	plan := mailPlan{goMod: goMod}
	change := func(path string, old, updated []byte, perm fs.FileMode) {
		if !bytes.Equal(old, updated) {
			plan.writes = append(plan.writes, fileWrite{path: path, content: updated, perm: perm})
		}
	}
	layout, err := mailLayout(dir)
	if err != nil {
		return mailPlan{}, fmt.Errorf("the app %w", err)
	}
	type providerFile struct {
		path    string
		content []byte
	}
	files := []providerFile{{recipes.InfraMailPath, r.InfraMail}, {recipes.InfraMailTestPath, r.InfraMailTest}}
	modules := r.Modules
	if layout == recipes.LayoutV02 {
		// gorbital delivers development email itself, so the app needs only
		// the provider's module.
		files, modules = []providerFile{{recipes.MainMailPath, r.MainMail}}, []string{"gorbital.dev/modules/mail/" + r.Provider}
	}

	for _, f := range files {
		old, _, err := readOptional(filepath.Join(dir, f.path))
		if err != nil {
			return mailPlan{}, err
		}
		change(f.path, old, f.content, 0o644)
	}

	newExample, err := recipes.ReplaceBlock(example, recipes.MailBlock, r.EnvBlock)
	if err != nil {
		return mailPlan{}, err
	}
	change(envExamplePath, example, newExample, 0o644)

	values := in.envValues()
	env, envExists, err := readOptional(filepath.Join(dir, envPath))
	if err != nil {
		return mailPlan{}, err
	}
	switch {
	case envExists:
		change(envPath, env, updateDotEnv(env, r.EnvBlock, values), 0o600)
	case len(values) > 0:
		// Start .env from the updated example, then fill in the values.
		plan.writes = append(plan.writes, fileWrite{path: envPath, content: updateDotEnv(newExample, r.EnvBlock, values), perm: 0o600})
		plan.createsEnv = true
	}
	for _, key := range r.EnvKeys {
		if _, ok := values[key]; ok {
			plan.envVars = append(plan.envVars, key)
		}
	}

	manifest, manifestExists, err := readOptional(filepath.Join(dir, manifestPath))
	if err != nil {
		return mailPlan{}, err
	}
	if manifestExists {
		change(manifestPath, manifest, recipes.SetManifestKey(manifest, "mail", r.Provider), 0o644)
	}

	// A v2 lock records the provider and the new content of the files it
	// tracks, so upgrades rebuild this provider's files (ADR-0050). A v1 lock
	// can't record it; gorbital.yaml holds the provider for those apps.
	lock, err := readLock(dir)
	switch {
	case err == nil && lock.APIVersion == LockAPIVersion && lock.rendered():
		lock.Inputs.Mail = r.Provider
		for _, w := range plan.writes {
			lock.record(w.path, w.content)
		}
		updated, err := lock.encode()
		if err != nil {
			return mailPlan{}, err
		}
		old, _, err := readOptional(filepath.Join(dir, lockPath))
		if err != nil {
			return mailPlan{}, err
		}
		change(lockPath, old, updated, 0o644)
	case err != nil && !errors.Is(err, errNoLock):
		return mailPlan{}, err
	}

	for _, module := range modules {
		if !slices.ContainsFunc(plan.goMod.Require, func(req goModRequire) bool { return req.Path == module }) {
			plan.modules = append(plan.modules, module)
		}
	}
	return plan, nil
}

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return data, err == nil, err
}

func applyMail(ctx context.Context, dir string, plan mailPlan, allowDirty, skipTidy bool, stderr io.Writer) error {
	if !allowDirty {
		if err := requireCleanGit(ctx, dir); err != nil {
			return err
		}
	}
	if plan.writesEnv() && insideGitRepo(ctx, dir) {
		cmd := exec.CommandContext(ctx, "git", "check-ignore", "--quiet", envPath)
		cmd.Dir = dir
		if cmd.Run() != nil {
			return errors.New(".env is not ignored by git, so secrets saved in it could be committed; add .env to .gitignore and run orb add mail again")
		}
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, w := range plan.writes {
		if err := root.WriteFile(w.path, w.content, w.perm); err != nil {
			return fmt.Errorf("write %s: %w", w.path, err)
		}
		if err := restrictSecretFile(root, w, stderr); err != nil {
			return err
		}
	}

	if len(plan.modules) > 0 {
		args := []string{"mod", "edit"}
		version, local := plan.goMod.gorbital()
		for _, module := range plan.modules {
			args = append(args, "-require="+module+"@"+version)
			if local != "" {
				args = append(args, "-replace="+module+"="+path.Join(filepath.ToSlash(local), strings.TrimPrefix(module, "gorbital.dev/")))
			}
		}
		if err := runIn(ctx, dir, stderr, "go", args...); err != nil {
			return fmt.Errorf("update go.mod: %w", err)
		}
	}
	if !skipTidy {
		if err := runIn(ctx, dir, stderr, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("files are updated, but go mod tidy failed: %w", err)
		}
	}
	return nil
}

// restrictSecretFile gives a file written with private permissions, such as
// .env, those permissions even when it already existed: WriteFile keeps an
// existing file's mode, and cp .env.example .env makes it readable by
// everyone. It warns when it narrows them, since the file may have been
// copied or backed up while readable.
func restrictSecretFile(root *os.Root, w fileWrite, stderr io.Writer) error {
	if w.perm&0o077 != 0 {
		return nil
	}
	info, err := root.Stat(w.path)
	if err != nil {
		return fmt.Errorf("check the permissions of %s: %w", w.path, err)
	}
	if info.Mode().Perm()&^w.perm == 0 {
		return nil
	}
	if err := root.Chmod(w.path, w.perm); err != nil {
		return fmt.Errorf("make %s private: %w", w.path, err)
	}
	fmt.Fprintf(stderr, "orb: %s was readable by other users (mode %04o); it holds secrets, so it is now %04o\n", w.path, info.Mode().Perm(), w.perm)
	return nil
}

type goModRequire struct {
	Path    string
	Version string
}

type goModInfo struct {
	// Go is the go directive's version, such as 1.26.0.
	Go      string
	Module  struct{ Path string }
	Require []goModRequire
	Replace []struct {
		Old struct{ Path string }
		New struct{ Path, Version string }
	}
}

// gorbital returns the version the app requires the library at, and the
// local checkout it replaces the library with, if any.
func (g goModInfo) gorbital() (version, local string) {
	version = "v0.0.0"
	for _, r := range g.Require {
		if r.Path == "gorbital.dev" {
			version = r.Version
		}
	}
	for _, r := range g.Replace {
		if r.Old.Path == "gorbital.dev" && r.New.Version == "" {
			local = r.New.Path
		}
	}
	return version, local
}

func readGoMod(ctx context.Context, dir string) (goModInfo, error) {
	cmd := exec.CommandContext(ctx, "go", "mod", "edit", "-json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return goModInfo{}, fmt.Errorf("read go.mod: %w", err)
	}
	var info goModInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return goModInfo{}, fmt.Errorf("read go.mod: %w", err)
	}
	return info, nil
}

// updateDotEnv puts the provider block into a .env file. Each variable keeps
// the value it already had anywhere in the file unless values sets a new
// one; assignments of the block's variables outside the block are removed,
// so each is defined once. Without a block, it is appended.
func updateDotEnv(env, block []byte, values map[string]string) []byte {
	blockKeys := map[string]bool{}
	for _, line := range strings.SplitAfter(string(block), "\n") {
		if key, ok := recipes.EnvKey(line); ok {
			blockKeys[key] = true
		}
	}

	existing := map[string]string{}
	var kept strings.Builder
	inBlock := false
	for _, line := range strings.SplitAfter(string(env), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# orb:begin "+recipes.MailBlock), strings.HasPrefix(trimmed, "# "+recipes.LegacyMarker+":begin "+recipes.MailBlock):
			inBlock = true
		case trimmed == "# orb:end "+recipes.MailBlock, trimmed == "# "+recipes.LegacyMarker+":end "+recipes.MailBlock:
			inBlock = false
		}
		if key, ok := recipes.EnvKey(line); ok {
			if _, value, _ := strings.Cut(strings.TrimRight(line, "\r\n"), "="); strings.TrimSpace(value) != "" {
				existing[key] = strings.TrimSpace(value)
			}
			if blockKeys[key] && !inBlock {
				continue
			}
		}
		kept.WriteString(line)
	}

	var filled strings.Builder
	for _, line := range strings.SplitAfter(string(block), "\n") {
		if key, ok := recipes.EnvKey(line); ok {
			if v, given := values[key]; given {
				line = key + "=" + quoteEnvValue(v) + "\n"
			} else if v, had := existing[key]; had {
				line = key + "=" + v + "\n"
			}
		}
		filled.WriteString(line)
	}

	updated, err := recipes.ReplaceBlock([]byte(kept.String()), recipes.MailBlock, []byte(filled.String()))
	if errors.Is(err, recipes.ErrBlockMissing) {
		out := kept.String()
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return []byte(out + "\n" + filled.String())
	}
	return updated
}

// quoteEnvValue quotes values the .env parser would otherwise cut short.
func quoteEnvValue(v string) string {
	if !strings.ContainsAny(v, " #\"'") {
		return v
	}
	if !strings.Contains(v, `"`) {
		return `"` + v + `"`
	}
	return "'" + v + "'"
}

func mailSummary(in mailInput, r recipes.MailRecipe, plan mailPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Provider:  %s\n", r.Label)
	if in.provider == recipes.MailSMTP {
		server := "not set yet (add SMTP_HOST to .env)"
		if in.smtpHost != "" {
			server = net.JoinHostPort(in.smtpHost, in.smtpPort) + " (" + in.smtpTLS + ")"
		}
		fmt.Fprintf(&b, "  Server:    %s\n", server)
	}
	b.WriteString("  Changes:\n")
	for _, w := range plan.writes {
		var notes []string
		if w.path == envPath {
			if plan.createsEnv {
				notes = append(notes, "created from .env.example")
			}
			if len(plan.envVars) > 0 {
				notes = append(notes, "saves "+strings.Join(plan.envVars, ", "))
			} else {
				notes = append(notes, "updates the email block")
			}
		}
		line := "    " + w.path
		if len(notes) > 0 {
			line += " (" + strings.Join(notes, ", ") + ")"
		}
		b.WriteString(line + "\n")
	}
	if len(plan.modules) > 0 {
		fmt.Fprintf(&b, "    go.mod (adds %s)\n", strings.Join(plan.modules, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// mailNextSteps tells the user exactly what is left to do.
func mailNextSteps(dir string, in mailInput, plan mailPlan) string {
	saved := map[string]bool{}
	for _, key := range plan.envVars {
		saved[key] = true
	}
	var steps []string
	switch in.provider {
	case recipes.MailResend:
		if saved["RESEND_API_KEY"] {
			steps = append(steps, "✓ Your Resend API key is saved in .env (RESEND_API_KEY)")
		} else {
			steps = append(steps, "Create an API key at https://resend.com/api-keys and add it to .env:\n       RESEND_API_KEY=re_…")
		}
		steps = append(steps, "Verify the domain you send from at https://resend.com/domains",
			"Optional, so addresses that bounce or complain stop receiving email: add a webhook at https://resend.com/webhooks\n"+
				"       for email.bounced and email.complained, pointing at https://<your API>/v1/webhooks/resend,\n"+
				"       and put its signing secret in .env: RESEND_WEBHOOK_SECRET=whsec_…")
	case recipes.MailSMTP:
		switch {
		case !saved["SMTP_HOST"]:
			steps = append(steps, "Add your SMTP server to .env: SMTP_HOST, SMTP_PORT, SMTP_TLS, SMTP_USERNAME and SMTP_PASSWORD")
		case in.smtpUsername != "" && !saved["SMTP_PASSWORD"]:
			steps = append(steps, "✓ The SMTP server is saved in .env\n     Add the password to .env: SMTP_PASSWORD=…")
		default:
			steps = append(steps, "✓ The SMTP server is saved in .env")
		}
		steps = append(steps, "Make sure your SMTP provider allows the address you send from")
	}
	steps = append(steps,
		"Start the app: docker compose up -d --wait, "+migrateCommand(dir)+", go run ./cmd/api",
		"Set the sender in the admin API; it applies at once, without a restart:\n"+
			`       PUT /ops/settings/mail.from_email  {"value": "hello@yourdomain.com", "version": 0, "reason": "our domain"}`+"\n"+
			`       PUT /ops/settings/mail.from_name   {"value": "Your App", "version": 0, "reason": "our name"}`+"\n"+
			`       PUT /ops/settings/mail.reply_to    (optional)`,
		"Send yourself a test email:\n"+
			`       POST /ops/mail/test  {"to": "you@example.com"}`,
	)

	var b strings.Builder
	b.WriteString("Next steps:\n")
	for i, step := range steps {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
	}
	b.WriteString("\nIn development every email goes to Mailpit: open http://127.0.0.1:8025.\n" +
		"To send real email while developing, set MAIL_DELIVERY=provider in .env.\n")
	return b.String()
}
