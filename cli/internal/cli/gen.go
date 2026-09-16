package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/recipes"
)

const genUsage = `Usage:
  orb gen job <Name> [flags]
  orb gen resource <Name> <field:type>... [flags]
  orb gen migration <name> [flags]

job generates a background job whose schedule, timeout and retries can be
changed at runtime through /ops/jobs. resource generates a module, table and
API for records that belong to the signed-in user. migration creates an empty
database migration that runs after the existing ones. Run them inside an app
created with the Full preset. In a terminal, missing values are asked
interactively; pass flags to skip them.
`

func runGen(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(stderr, genUsage)
		return usageError("missing generator: orb gen job <Name>, orb gen resource <Name> <field:type>... or orb gen migration <name>")
	}
	switch args[0] {
	case "job":
		return runGenJob(ctx, args[1:], stdin, stdout, stderr)
	case "resource":
		return runGenResource(ctx, args[1:], stdin, stdout, stderr)
	case "migration":
		return runGenMigration(ctx, args[1:], stdin, stdout, stderr)
	default:
		return usageError(fmt.Sprintf("unknown generator %q (want job, resource or migration)", args[0]))
	}
}

// jobInput holds the job's settings as the user supplies them.
type jobInput struct {
	name        string
	description string
	trigger     string // schedule, interval or manual
	schedule    string
	every       string
	timeout     string
	maxAttempts int
	queue       string
	priority    int
	enabled     bool
}

type genJobResult struct {
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Files      []string `json:"files"`
	DryRun     bool     `json:"dry_run"`
}

const (
	triggerSchedule = "schedule"
	triggerInterval = "interval"
	triggerManual   = "manual"
	customChoice    = "custom"
)

func runGenJob(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb gen job", flag.ContinueOnError)
	flags.SetOutput(stderr)
	in := jobInput{}
	flags.StringVar(&in.description, "description", "", `what the job does (default "<Name> job.")`)
	flags.StringVar(&in.schedule, "schedule", "", `cron schedule in UTC, such as "0 3 * * *" or "@daily"`)
	flags.StringVar(&in.every, "every", "", "run at a fixed interval instead, such as 15m (at least 1m)")
	onDemand := flags.Bool("on-demand", false, "no schedule: run from code or with POST /ops/jobs/definitions/<name>/run")
	flags.StringVar(&in.timeout, "timeout", "1m", "longest time one attempt may run, 1s to 24h")
	flags.IntVar(&in.maxAttempts, "max-attempts", 5, "attempts before the job is discarded, 1 to 100")
	flags.StringVar(&in.queue, "queue", "default", "queue the job runs on")
	flags.IntVar(&in.priority, "priority", 1, "priority within the queue, 1 (highest) to 4")
	disabled := flags.Bool("disabled", false, "create the job disabled")
	dryRun := flags.Bool("dry-run", false, "show what would be generated without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		in.name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if in.name == "" && flags.NArg() > 0 {
		in.name = flags.Arg(0)
	} else if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	in.enabled = !*disabled

	triggers := 0
	for _, name := range []string{"schedule", "every", "on-demand"} {
		if set[name] {
			triggers++
		}
	}
	switch {
	case triggers > 1:
		return usageError("use only one of --schedule, --every and --on-demand")
	case set["schedule"]:
		in.trigger = triggerSchedule
	case set["every"]:
		in.trigger = triggerInterval
	case *onDemand:
		in.trigger = triggerManual
	}

	app, err := findApp()
	if err != nil {
		return err
	}

	if shouldPrompt(p, *asJSON, stdin, stdout) {
		if err := promptJob(&in, set, p, stdin, stderr); err != nil {
			return err
		}
	} else if in.name == "" {
		return usageError("missing job name: orb gen job <Name> (or run it in a terminal to be asked)")
	}
	if in.trigger == "" {
		in.trigger = triggerSchedule
		in.schedule = "0 3 * * *"
	}

	data, err := jobData(app.module, in)
	if err != nil {
		return err
	}
	files, err := recipes.RenderJob(data)
	if err != nil {
		return err
	}
	jobsGo := filepath.Join("internal", "app", "jobs.go")
	src, err := os.ReadFile(filepath.Join(app.dir, jobsGo))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s has no %s: orb gen job works in apps created with the Full preset", app.dir, jobsGo)
	} else if err != nil {
		return err
	}
	callLine := "define" + data.Ident + "Job(defs, deps)"
	updated, err := recipes.InsertAfterAnchor(src, recipes.JobAnchor, callLine)
	if errors.Is(err, recipes.ErrAnchorMissing) {
		return fmt.Errorf("%s has no %q line; add it inside defineJobs, then run orb gen job again", jobsGo, recipes.JobAnchor)
	} else if err != nil {
		return fmt.Errorf("%s: job %s is already registered: %w", jobsGo, data.Name, err)
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	result := genJobResult{Name: data.Ident, Definition: data.Name, DryRun: *dryRun}
	for _, f := range files {
		if _, err := root.Stat(f.Path); err == nil {
			return fmt.Errorf("%s already exists; choose another job name", f.Path)
		}
		result.Files = append(result.Files, f.Path)
	}
	result.Files = append(result.Files, filepath.ToSlash(jobsGo))

	summary := jobSummary(data, result.Files)
	if !*dryRun && shouldPrompt(p, *asJSON, stdin, stdout) {
		ok, err := confirm("Generate this job?", summary, p, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	}

	if !*dryRun {
		if !*allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		for _, f := range files {
			if err := root.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
				return err
			}
			if err := root.WriteFile(f.Path, f.Content, 0o644); err != nil {
				return err
			}
		}
		if err := root.WriteFile(jobsGo, updated, 0o644); err != nil {
			return err
		}
	}

	if *asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Created"
	if *dryRun {
		verb = "Would create (dry run)"
	}
	fmt.Fprintf(stdout, "✓ %s job %s\n\n%s\n", verb, data.Name, summary)
	if !*dryRun {
		fmt.Fprintf(stdout, "\nNext:\n  1. Write the job in internal/jobs/%s/%s.go (Work)\n  2. go test ./internal/app -run TestPublicSurface -update (records the job name)\n  3. go test ./...\n  4. go run ./cmd/api\n\n"+
			"Change its schedule, timeout or retries any time, without a deploy:\n  PUT /ops/jobs/definitions/%s\n", data.Package, data.Package, data.Name)
	}
	return nil
}

// promptJob asks for every value not given by a flag. It asks in short
// steps, so follow-up questions (a custom cron expression, a custom interval)
// appear only when they apply, in both the arrow-key and plain modes.
func promptJob(in *jobInput, set map[string]bool, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	ask := func(fields ...huh.Field) error {
		if len(fields) == 0 {
			return nil
		}
		return runForm(huh.NewForm(huh.NewGroup(fields...)), p, stdin, stderr)
	}
	askTrigger := in.trigger == ""

	// Step 1: what the job is and when it runs.
	var step []huh.Field
	if in.name == "" {
		step = append(step, huh.NewInput().Title("Job name").
			Description("A Go-style name, such as CleanupSessions or SendWeeklyReport.").
			Placeholder("CleanupSessions").Value(&in.name).
			Validate(func(s string) error { _, err := jobNames(s); return err }))
	}
	if !set["description"] {
		step = append(step, huh.NewInput().Title("What does it do?").
			Description("Shown to operators in /ops/jobs. Leave empty to fill in later.").
			Value(&in.description).Validate(validateDescription))
	}
	if askTrigger {
		in.trigger = triggerSchedule
		step = append(step, huh.NewSelect[string]().Title("When should it run?").Options(
			huh.NewOption("On a schedule (cron, UTC)", triggerSchedule),
			huh.NewOption("At a fixed interval", triggerInterval),
			huh.NewOption("Only on demand (from code or the admin panel)", triggerManual),
		).Value(&in.trigger))
	}
	if err := ask(step...); err != nil {
		return err
	}

	// Step 2: the schedule or interval, with a custom value only if chosen.
	if askTrigger {
		switch in.trigger {
		case triggerSchedule:
			preset := "0 3 * * *"
			if err := ask(huh.NewSelect[string]().Title("Schedule").
				Description("You can change it later in /ops/jobs without a deploy.").Options(
				huh.NewOption("Every day at 03:00 UTC", "0 3 * * *"),
				huh.NewOption("Every hour", "@hourly"),
				huh.NewOption("Every Monday at 09:00 UTC", "0 9 * * 1"),
				huh.NewOption("First day of the month at 00:00 UTC", "@monthly"),
				huh.NewOption("Custom cron expression…", customChoice),
			).Value(&preset)); err != nil {
				return err
			}
			in.schedule = preset
			if preset == customChoice {
				in.schedule = ""
				if err := ask(huh.NewInput().Title("Cron expression").
					Description(`Five fields in UTC: minute hour day month weekday, such as "30 2 * * *".`).
					Value(&in.schedule).Validate(validateSchedule)); err != nil {
					return err
				}
			}
		case triggerInterval:
			preset := "1h"
			if err := ask(huh.NewSelect[string]().Title("Interval").Options(
				huh.NewOption("Every 5 minutes", "5m"),
				huh.NewOption("Every 15 minutes", "15m"),
				huh.NewOption("Every 30 minutes", "30m"),
				huh.NewOption("Every hour", "1h"),
				huh.NewOption("Every 6 hours", "6h"),
				huh.NewOption("Custom…", customChoice),
			).Value(&preset)); err != nil {
				return err
			}
			in.every = preset
			if preset == customChoice {
				in.every = ""
				if err := ask(huh.NewInput().Title("Interval").
					Description("A duration of at least 1m, such as 90m.").
					Value(&in.every).Validate(validateInterval)); err != nil {
					return err
				}
			}
		}
	}

	// Step 3: limits and whether the job starts enabled.
	step = nil
	if !set["timeout"] {
		in.timeout = "1m"
		step = append(step, huh.NewSelect[string]().Title("Timeout per attempt").Options(
			huh.NewOption("30 seconds", "30s"),
			huh.NewOption("1 minute", "1m"),
			huh.NewOption("5 minutes", "5m"),
			huh.NewOption("15 minutes", "15m"),
			huh.NewOption("1 hour", "1h"),
		).Value(&in.timeout))
	}
	if !set["max-attempts"] {
		step = append(step, huh.NewSelect[int]().Title("Attempts before giving up").Options(
			huh.NewOption("1 (never retry)", 1),
			huh.NewOption("3", 3),
			huh.NewOption("5", 5),
			huh.NewOption("10", 10),
			huh.NewOption("25", 25),
		).Value(&in.maxAttempts))
	}
	if !set["disabled"] {
		step = append(step, huh.NewConfirm().Title("Enable the job now?").
			Description("Disabled jobs don't run on their schedule until enabled in /ops/jobs.").
			Affirmative("Enabled").Negative("Disabled").Value(&in.enabled))
	}
	return ask(step...)
}

// jobData validates the input and derives every name.
func jobData(module string, in jobInput) (recipes.JobData, error) {
	names, err := jobNames(in.name)
	if err != nil {
		return recipes.JobData{}, usageError(err.Error())
	}
	if err := validateDescription(in.description); err != nil {
		return recipes.JobData{}, usageError("--description " + err.Error())
	}
	description := strings.TrimSpace(in.description)
	if description == "" {
		description = names.ident + " job."
	}

	schedule := ""
	switch in.trigger {
	case triggerSchedule:
		if err := validateSchedule(in.schedule); err != nil {
			return recipes.JobData{}, usageError("--schedule " + err.Error())
		}
		schedule = strings.TrimSpace(in.schedule)
	case triggerInterval:
		if err := validateInterval(in.every); err != nil {
			return recipes.JobData{}, usageError("--every " + err.Error())
		}
		d, _ := time.ParseDuration(strings.TrimSpace(in.every))
		schedule = "@every " + formatDuration(d)
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(in.timeout))
	if err != nil || timeout < time.Second || timeout > 24*time.Hour {
		return recipes.JobData{}, usageError("--timeout must be a duration between 1s and 24h, such as 5m")
	}
	if in.maxAttempts < 1 || in.maxAttempts > 100 {
		return recipes.JobData{}, usageError("--max-attempts must be between 1 and 100")
	}
	if !queuePattern.MatchString(in.queue) {
		return recipes.JobData{}, usageError("--queue must be 1 to 100 letters, digits, hyphens or underscores")
	}
	if in.priority < 1 || in.priority > 4 {
		return recipes.JobData{}, usageError("--priority must be between 1 and 4")
	}
	return recipes.JobData{
		Module:      module,
		Ident:       names.ident,
		Name:        names.name,
		Package:     names.pkg,
		Description: description,
		Enabled:     in.enabled,
		Schedule:    schedule,
		Timeout:     timeout,
		MaxAttempts: in.maxAttempts,
		Queue:       in.queue,
		Priority:    in.priority,
	}, nil
}

var (
	jobNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	queuePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	cronField      = regexp.MustCompile(`^[0-9A-Za-z*/,?-]+$`)
)

type jobNameSet struct {
	ident string // CleanupSessions
	name  string // cleanup_sessions
	pkg   string // cleanupsessions
}

// jobNames splits a name such as CleanupSessions, cleanup-sessions or
// cleanup_sessions into its Go identifier, definition name and package name.
func jobNames(input string) (jobNameSet, error) {
	input = strings.TrimSpace(input)
	if len(input) == 0 || len(input) > 60 || !jobNamePattern.MatchString(input) {
		return jobNameSet{}, errors.New("job name must start with a letter and use letters, digits, hyphens or underscores (max 60), such as CleanupSessions")
	}
	words := splitWords(input)

	var ident strings.Builder
	for _, w := range words {
		ident.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	set := jobNameSet{ident: ident.String(), name: strings.Join(words, "_"), pkg: strings.Join(words, "")}
	switch {
	case len(set.name) > 63:
		return jobNameSet{}, errors.New("job name is too long")
	case token.IsKeyword(set.pkg) || !token.IsIdentifier(set.pkg) || !token.IsIdentifier(set.ident):
		return jobNameSet{}, fmt.Errorf("job name %q can't be used as a Go package name; choose another", input)
	}
	return set, nil
}

func validateDescription(s string) error {
	switch {
	case strings.ContainsAny(s, "\r\n"):
		return errors.New("must be a single line")
	case len(s) > 200:
		return errors.New("must be at most 200 characters")
	}
	return nil
}

var cronDescriptors = map[string]bool{
	"@yearly": true, "@annually": true, "@monthly": true, "@weekly": true, "@daily": true, "@midnight": true, "@hourly": true,
}

// validateSchedule mirrors the checks modules/jobs applies at startup.
func validateSchedule(s string) error {
	s = strings.TrimSpace(s)
	if cronDescriptors[s] {
		return nil
	}
	if rest, ok := strings.CutPrefix(s, "@every "); ok {
		return validateInterval(rest)
	}
	fields := strings.Fields(s)
	if len(fields) == 5 {
		valid := true
		for _, f := range fields {
			valid = valid && cronField.MatchString(f)
		}
		if valid {
			return nil
		}
	}
	return errors.New(`must be a 5-field cron expression such as "0 3 * * *", or a descriptor such as "@daily"`)
}

func validateInterval(s string) error {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < time.Minute {
		return errors.New("must be a duration of at least 1m, such as 15m or 2h")
	}
	return nil
}

// formatDuration drops zero trailing units: 90m → 1h30m, 1h → 1h, 10s → 10s.
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") || strings.HasSuffix(s, "h0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

func jobSummary(d recipes.JobData, files []string) string {
	var b strings.Builder
	trigger := "on demand"
	if d.Schedule != "" {
		trigger = d.Schedule
		if !strings.HasPrefix(d.Schedule, "@") {
			trigger += " (UTC)"
		}
	}
	state := "enabled"
	if !d.Enabled {
		state = "disabled"
	}
	fmt.Fprintf(&b, "  Job:       %s (%s)\n", d.Name, state)
	fmt.Fprintf(&b, "  Runs:      %s\n", trigger)
	fmt.Fprintf(&b, "  Timeout:   %s · %d attempts · queue %s · priority %d\n", formatDuration(d.Timeout), d.MaxAttempts, d.Queue, d.Priority)
	b.WriteString("  Files:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "    %s\n", f)
	}
	return strings.TrimRight(b.String(), "\n")
}

type appInfo struct {
	dir    string
	module string
}

// findApp returns the nearest directory at or above the current one with a
// go.mod, and its module path.
func findApp() (appInfo, error) {
	dir, err := os.Getwd()
	if err != nil {
		return appInfo{}, err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			module := modulePath(data)
			if module == "" {
				return appInfo{}, fmt.Errorf("%s/go.mod has no module line", dir)
			}
			return appInfo{dir: dir, module: module}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return appInfo{}, errors.New("no go.mod found: run orb gen job inside an gorbital app")
		}
		dir = parent
	}
}

func modulePath(goMod []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(goMod))
	for scanner.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			if unquoted, err := strconv.Unquote(rest); err == nil {
				return unquoted
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// requireCleanGit refuses to generate into a git repository with
// uncommitted changes, so the generated diff is easy to review (ADR-0021).
func requireCleanGit(ctx context.Context, dir string) error {
	if !insideGitRepo(ctx, dir) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("check git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return errors.New("the git repository has uncommitted changes; commit or stash them first, or pass --allow-dirty")
	}
	return nil
}

// insideGitRepo reports whether dir is inside a git work tree. Without git
// installed, it reports false.
func insideGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
