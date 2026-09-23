package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
)

// These tests drive the question flows in plain (accessible) mode, which
// reads one answer per line, so they run without a terminal. Select answers
// are option numbers; confirms are y or n.

// lineReader returns one line per Read, like a terminal delivering each
// answer as it is typed. Plain prompts read each answer with their own
// scanner, which would otherwise swallow later answers from a single buffer.
type lineReader struct {
	lines []string
}

func answers(lines ...string) *lineReader { return &lineReader{lines: lines} }

func (r *lineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.lines[0]+"\n")
	r.lines = r.lines[1:]
	return n, nil
}

func TestPromptJobAsksForMissingValues(t *testing.T) {
	stdin := answers(
		"NightlyReport",             // job name
		"Sends the nightly report.", // description
		"1",                         // when: on a schedule
		"2",                         // schedule: every hour
		"3",                         // timeout: 5 minutes
		"4",                         // attempts: 10
		"n",                         // enabled: no
	)
	in := jobInput{timeout: "1m", maxAttempts: 5, queue: "default", priority: 1, enabled: true}
	var out bytes.Buffer
	if err := promptJob(&in, map[string]bool{}, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptJob() error = %v\noutput:\n%s", err, out.String())
	}

	want := jobInput{
		name: "NightlyReport", description: "Sends the nightly report.",
		trigger: triggerSchedule, schedule: "@hourly",
		timeout: "5m", maxAttempts: 10, queue: "default", priority: 1, enabled: false,
	}
	if in != want {
		t.Errorf("answers = %+v\nwant      %+v\noutput:\n%s", in, want, out.String())
	}
	for _, question := range []string{"Job name", "What does it do?", "When should it run?", "Schedule", "Timeout per attempt", "Attempts before giving up", "Enable the job now?"} {
		if !strings.Contains(out.String(), question) {
			t.Errorf("question %q was not asked; output:\n%s", question, out.String())
		}
	}
	for _, notAsked := range []string{"Cron expression", "Interval"} {
		if strings.Contains(out.String(), notAsked) {
			t.Errorf("question %q doesn't apply to a preset schedule but was asked; output:\n%s", notAsked, out.String())
		}
	}
}

func TestPromptJobCustomInterval(t *testing.T) {
	stdin := answers(
		"SyncCatalog", // job name
		"",            // description: fill in later
		"2",           // when: at a fixed interval
		"6",           // interval: custom
		"90m",         // custom interval
		"2",           // timeout: 1 minute
		"2",           // attempts: 3
		"y",           // enabled
	)
	in := jobInput{timeout: "1m", maxAttempts: 5, queue: "default", priority: 1, enabled: true}
	var out bytes.Buffer
	if err := promptJob(&in, map[string]bool{}, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptJob() error = %v\noutput:\n%s", err, out.String())
	}
	if in.trigger != triggerInterval || in.every != "90m" || in.schedule != "" || in.maxAttempts != 3 || !in.enabled {
		t.Errorf("answers = %+v\noutput:\n%s", in, out.String())
	}
	if strings.Contains(out.String(), "Schedule") || strings.Contains(out.String(), "Cron expression") {
		t.Errorf("schedule questions asked for an interval job; output:\n%s", out.String())
	}

	data, err := jobData("example.com/shop", in)
	if err != nil || data.Schedule != "@every 1h30m" || data.Description != "SyncCatalog job." {
		t.Errorf("jobData() = %+v, %v", data, err)
	}
}

func TestPromptJobSkipsQuestionsAnsweredByFlags(t *testing.T) {
	// Only the description and the enabled toggle remain unanswered.
	stdin := answers("Rebuilds the search index.", "y")
	in := jobInput{name: "RebuildIndex", trigger: triggerInterval, every: "30m", timeout: "2m", maxAttempts: 3, queue: "default", priority: 1, enabled: true}
	set := map[string]bool{"every": true, "timeout": true, "max-attempts": true}

	var out bytes.Buffer
	if err := promptJob(&in, set, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptJob() error = %v\noutput:\n%s", err, out.String())
	}
	if in.description != "Rebuilds the search index." || in.every != "30m" || in.timeout != "2m" || in.maxAttempts != 3 || !in.enabled {
		t.Errorf("answers = %+v", in)
	}
	for _, skipped := range []string{"Job name", "When should it run?", "Interval", "Timeout per attempt", "Attempts before giving up"} {
		if strings.Contains(out.String(), skipped) {
			t.Errorf("question %q was asked although its flag was given; output:\n%s", skipped, out.String())
		}
	}
}

func TestPromptNewAsksForMissingValues(t *testing.T) {
	stdin := answers(
		"shop-api", // app name
		"2",        // preset: full
		"3",        // sign-in: full
		"3",        // tenancy: named
		"merchant", // what a tenant is called
		"n",        // git init: no
	)

	var name, module, local string
	preset, auth, scope := "minimal", "", ""
	noGit := false
	var out bytes.Buffer
	err := promptNew(&name, &module, &preset, &auth, &scope, &local, &noGit, map[string]bool{}, promptFlags{plain: true}, stdin, &out)
	if err != nil {
		t.Fatalf("promptNew() error = %v\noutput:\n%s", err, out.String())
	}
	if name != "shop-api" || module != "" || preset != "full" || auth != "full" || scope != "merchant" || local != "" || !noGit {
		t.Errorf("answers = name %q, module %q, preset %q, auth %q, scope %q, local %q, noGit %v\noutput:\n%s", name, module, preset, auth, scope, local, noGit, out.String())
	}
	// The module path defaults to the app name and is never asked: --module sets it.
	if s := out.String(); strings.Contains(s, "module path") {
		t.Errorf("prompts asked for a Go module path:\n%s", s)
	}
	// Without --local the published library is used, not asked for.
	if s := out.String(); strings.Contains(s, "gorbital checkout") {
		t.Errorf("prompts asked for a gorbital checkout:\n%s", s)
	}
	// Plain mode prints each answer itself: no folded lines, no colour.
	if s := out.String(); strings.Contains(s, "✓") || strings.Contains(s, "\x1b[") {
		t.Errorf("plain prompts printed folded lines or colour:\n%s", s)
	}
}

func TestPromptNewShowsTheModulePathWithoutAsking(t *testing.T) {
	for _, tc := range []struct {
		name, module, want string
		set                map[string]bool
	}{
		{"default", "", "✓ Go module path … shop-api\n", map[string]bool{}},
		{"flag", "github.com/acme/shop-api", "✓ Go module path … github.com/acme/shop-api\n", map[string]bool{"module": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Every other question is answered by flag, so the prompts run
			// no form: a module question would fail on the empty input.
			name, module, preset, local := "shop-api", tc.module, "minimal", ""
			var auth, scope string
			noGit := true
			set := map[string]bool{"preset": true, "no-git": true}
			for k := range tc.set {
				set[k] = true
			}
			var out bytes.Buffer
			if err := promptNew(&name, &module, &preset, &auth, &scope, &local, &noGit, set, promptFlags{}, answers(), &out); err != nil {
				t.Fatalf("promptNew() error = %v\noutput:\n%s", err, out.String())
			}
			if module != tc.module || !noGit {
				t.Errorf("module = %q, noGit = %v; want %q, true", module, noGit, tc.module)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out.String())
			}
		})
	}
}

func TestPromptsStayInsideTheTerminal(t *testing.T) {
	// A line padded to the full width wraps in terminals that draw … or ↑↓
	// two columns wide, and every redraw then leaves a copy of the question.
	l := insetLayout{huh.LayoutDefault}
	for _, tc := range []struct{ terminal, want int }{{120, 117}, {80, 77}, {3, 1}, {0, 1}} {
		if got := l.GroupWidth(nil, nil, tc.terminal); got != tc.want {
			t.Errorf("GroupWidth(%d) = %d, want %d", tc.terminal, got, tc.want)
		}
	}
}

func TestAnsweredQuestionsFoldToOneLine(t *testing.T) {
	var out bytes.Buffer
	a := newAsker(promptFlags{}, answers(), &out)
	a.answered("app name", "shop-api")
	a.answered("initialise a git repository?", yesNo(false))

	// Output that isn't a terminal gets no colour, so the lines read as plain text.
	want := "✓ app name … shop-api\n✓ initialise a git repository? … no\n"
	if out.String() != want {
		t.Errorf("folded lines = %q, want %q", out.String(), want)
	}
	if got := a.title("app name"); got != "? app name … " {
		t.Errorf("title = %q, want %q", got, "? app name … ")
	}
}
