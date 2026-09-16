package jobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

// Bounds for job definition configuration (ADR-0033, threat 24).
const (
	MinScheduleInterval = time.Minute
	MinTimeout          = time.Second
	MaxTimeout          = 24 * time.Hour
	MaxAttemptsLimit    = 100
)

// Defaults for definition fields left zero.
const (
	DefaultDefinitionTimeout     = time.Minute
	DefaultDefinitionMaxAttempts = 25
)

var definitionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Config is a job definition's operator-editable configuration.
type Config struct {
	// Enabled jobs run on their schedule and can be run on demand. Jobs
	// enqueued by application code run either way.
	Enabled bool
	// Schedule is a 5-field cron expression evaluated in UTC ("0 3 * * *"),
	// a descriptor ("@daily", "@every 15m"), or empty for on-demand jobs.
	Schedule string
	// Timeout bounds each attempt.
	Timeout time.Duration
	// MaxAttempts is the number of attempts before the job is discarded.
	MaxAttempts int
	// Queue is the queue new jobs go to; workers must run it.
	Queue string
	// Priority orders jobs within a queue, 1 (highest) to 4.
	Priority int
}

// Validate reports whether c is within the allowed bounds.
func (c Config) Validate() error {
	var errs []error
	if _, err := parseSchedule(c.Schedule); err != nil {
		errs = append(errs, err)
	}
	if c.Timeout < MinTimeout || c.Timeout > MaxTimeout {
		errs = append(errs, fmt.Errorf("timeout must be between %v and %v", MinTimeout, MaxTimeout))
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > MaxAttemptsLimit {
		errs = append(errs, fmt.Errorf("max attempts must be between 1 and %d", MaxAttemptsLimit))
	}
	if c.Queue == "" {
		errs = append(errs, errors.New("queue is required"))
	}
	if c.Priority < 1 || c.Priority > 4 {
		errs = append(errs, errors.New("priority must be between 1 and 4"))
	}
	return errors.Join(errs...)
}

// A Definition declares a named job whose configuration operators can change
// at runtime, like a serverless function: the code is deployed, the settings
// are edited in the admin panel. Zero Timeout, MaxAttempts, Queue and Priority
// take the defaults.
type Definition[T river.JobArgs] struct {
	// Name identifies the job; T's Kind() must return it. Names are public
	// API: renaming one orphans its overrides and history.
	Name        string
	Description string
	// Worker runs the job. Its Timeout method is ignored: the definition's
	// timeout applies.
	Worker river.Worker[T]
	// NewArgs builds the arguments for scheduled and on-demand runs.
	NewArgs func() T

	Enabled     bool
	Schedule    string
	Timeout     time.Duration
	MaxAttempts int
	Queue       string
	Priority    int
}

type definition struct {
	name        string
	description string
	defaults    Config
	newArgs     func() river.JobArgs
	register    func(*river.Workers) error
}

// override holds operator changes; nil fields use the code default.
type override struct {
	enabled     *bool
	schedule    *string
	timeout     *time.Duration
	maxAttempts *int
	queue       *string
	priority    *int

	version   int64
	updatedAt time.Time
	updatedBy string
	// invalid marks an override that no longer passes validation, for example
	// after bounds tightened; the code defaults apply instead.
	invalid bool
}

// Definitions holds job definitions and the live overrides applied to them.
// Declare every definition before building the client. It is safe for
// concurrent use.
type Definitions struct {
	mu     sync.Mutex
	byName map[string]*definition
	names  []string
	frozen bool

	overridesMu sync.Mutex
	overrides   atomic.Pointer[map[string]override]
}

// NewDefinitions returns an empty set of definitions.
func NewDefinitions() *Definitions {
	return &Definitions{byName: make(map[string]*definition)}
}

// Define adds a job definition. Invalid definitions are programming errors
// found at startup, so Define panics.
func Define[T river.JobArgs](defs *Definitions, d Definition[T]) {
	fail := func(format string, args ...any) {
		panic(fmt.Sprintf("jobs: definition %q: ", d.Name) + fmt.Sprintf(format, args...))
	}
	if !definitionNamePattern.MatchString(d.Name) {
		fail("name must be lowercase snake_case, at most 63 characters")
	}
	if d.Worker == nil || d.NewArgs == nil {
		fail("Worker and NewArgs are required")
	}
	if kind := d.NewArgs().Kind(); kind != d.Name {
		fail("args Kind() returns %q; it must equal the definition name", kind)
	}
	cfg := Config{
		Enabled:     d.Enabled,
		Schedule:    d.Schedule,
		Timeout:     orDefault(d.Timeout, DefaultDefinitionTimeout),
		MaxAttempts: orDefault(d.MaxAttempts, DefaultDefinitionMaxAttempts),
		Queue:       orDefault(d.Queue, river.QueueDefault),
		Priority:    orDefault(d.Priority, 1),
	}
	if err := cfg.Validate(); err != nil {
		fail("%v", err)
	}

	def := &definition{
		name:        d.Name,
		description: d.Description,
		defaults:    cfg,
		newArgs:     func() river.JobArgs { return d.NewArgs() },
	}
	def.register = func(workers *river.Workers) error {
		return river.AddWorkerSafely[T](workers, &definedWorker[T]{inner: d.Worker, defs: defs, name: d.Name})
	}

	defs.mu.Lock()
	defer defs.mu.Unlock()
	if defs.frozen {
		fail("defined after the client was built; define every job first")
	}
	if _, dup := defs.byName[d.Name]; dup {
		fail("defined twice")
	}
	defs.byName[d.Name] = def
	defs.names = append(defs.names, d.Name)
}

func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// freeze registers every worker and stops further definitions.
func (defs *Definitions) freeze(workers *river.Workers) error {
	defs.mu.Lock()
	defer defs.mu.Unlock()
	if defs.frozen {
		return errors.New("definitions are already used by another client")
	}
	for _, name := range defs.names {
		if err := defs.byName[name].register(workers); err != nil {
			return fmt.Errorf("register %s worker: %w", name, err)
		}
	}
	defs.frozen = true
	return nil
}

// Names returns the name of every defined job, in definition order. Job
// names are public API (ADR-0015); apps record them in their surface
// inventory (ADR-0054).
func (defs *Definitions) Names() []string {
	defs.mu.Lock()
	defer defs.mu.Unlock()
	return slices.Clone(defs.names)
}

func (defs *Definitions) lookup(name string) (*definition, bool) {
	defs.mu.Lock()
	defer defs.mu.Unlock()
	d, ok := defs.byName[name]
	return d, ok
}

// list returns definitions in declaration order.
func (defs *Definitions) list() []*definition {
	defs.mu.Lock()
	defer defs.mu.Unlock()
	out := make([]*definition, len(defs.names))
	for i, name := range defs.names {
		out[i] = defs.byName[name]
	}
	return out
}

func (defs *Definitions) override(name string) (override, bool) {
	if m := defs.overrides.Load(); m != nil {
		o, ok := (*m)[name]
		return o, ok
	}
	return override{}, false
}

// effective merges a valid override into the code defaults.
func (defs *Definitions) effective(d *definition) Config {
	cfg := d.defaults
	o, ok := defs.override(d.name)
	if !ok || o.invalid {
		return cfg
	}
	return o.apply(cfg)
}

// configFor returns the effective configuration for a job kind.
func (defs *Definitions) configFor(kind string) (Config, bool) {
	d, ok := defs.lookup(kind)
	if !ok {
		return Config{}, false
	}
	return defs.effective(d), true
}

func (o override) apply(cfg Config) Config {
	if o.enabled != nil {
		cfg.Enabled = *o.enabled
	}
	if o.schedule != nil {
		cfg.Schedule = *o.schedule
	}
	if o.timeout != nil {
		cfg.Timeout = *o.timeout
	}
	if o.maxAttempts != nil {
		cfg.MaxAttempts = *o.maxAttempts
	}
	if o.queue != nil {
		cfg.Queue = *o.queue
	}
	if o.priority != nil {
		cfg.Priority = *o.priority
	}
	return cfg
}

// definedWorker runs a definition's worker with its live timeout.
type definedWorker[T river.JobArgs] struct {
	inner river.Worker[T]
	defs  *Definitions
	name  string
}

func (w *definedWorker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	return w.inner.Work(ctx, job)
}

func (w *definedWorker[T]) NextRetry(job *river.Job[T]) time.Time { return w.inner.NextRetry(job) }

func (w *definedWorker[T]) Middleware(job *rivertype.JobRow) []rivertype.WorkerMiddleware {
	return w.inner.Middleware(job)
}

func (w *definedWorker[T]) Timeout(*river.Job[T]) time.Duration {
	cfg, _ := w.defs.configFor(w.name)
	return cfg.Timeout
}

// parseSchedule parses a definition schedule; empty means no schedule.
func parseSchedule(spec string) (river.PeriodicSchedule, error) {
	if spec == "" {
		return nil, nil
	}
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, errors.New(`schedule must be a 5-field cron expression such as "0 3 * * *" or a descriptor such as "@every 15m"`)
	}
	if every, ok := s.(cron.ConstantDelaySchedule); ok && every.Delay < MinScheduleInterval {
		return nil, fmt.Errorf("schedule must not run more often than every %v", MinScheduleInterval)
	}
	return utcSchedule{s}, nil
}

// utcSchedule evaluates cron expressions in UTC on every instance.
type utcSchedule struct{ cron.Schedule }

func (s utcSchedule) Next(t time.Time) time.Time { return s.Schedule.Next(t.UTC()) }
