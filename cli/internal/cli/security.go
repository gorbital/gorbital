package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// orb doctor --security (ADR-0092 §7, roadmap items 38, 39 and 93).
//
// Since v0.2.1 orb new writes sign-in and organisations into the app as
// the app's own code, so a fix in the library does not reach them with go
// get. This is what makes that trade honest: gorbital.lock knows where
// each copy came from, so the copy can be told what it is missing, and the
// rules below read the app's own code for the mistakes that a copy is now
// free to make.
//
// Two things it deliberately does not do. It does not classify a change as
// a security fix: there is no advisory feed in this repository, so the
// honest output is the diff, the changelog and the operator's judgement.
// And it does not print a value it found — the location and the name, and
// nothing else, because a report that quotes a secret is a second copy of
// it (roadmap item 93).

// A securityResult is orb doctor --security.
type securityResult struct {
	App string `json:"app"`
	// Copies is one entry per module gorbital.lock records the app as
	// owning a copy of.
	Copies []securityCopy `json:"copies"`
	// Findings are the static rules' results, by file and line.
	Findings []securityFinding `json:"findings"`
	// Rules are the rules that ran, whether or not they found anything.
	Rules []securityRule `json:"rules"`
	// Checked and NotChecked are the report's last words, in the output
	// and here, so a script that gates on this can quote the limits along
	// with the verdict.
	Checked    []string `json:"checked"`
	NotChecked []string `json:"not_checked"`
	Failures   int      `json:"failures"`
	Warnings   int      `json:"warnings"`
}

const doctorSecurityUsage = `Usage: orb doctor --security [flags]

Reviews the code the app owns. For each module gorbital.lock records as
copied from the library, it fetches the version the copy was made from and
reports which of the files the app holds changed upstream since, which of
them the app has changed itself, and what the library's changelog says;
then it runs static rules over the app's own Go code and SQL migrations.

It cannot tell you whether a change upstream is a security fix: gorbital
publishes no advisory feed. It tells you that your copy has diverged and
where to read. The report ends with what it checked and what it did not.

Exit codes: 0 when nothing failed (warnings allowed), 1 when something did,
2 for invalid usage.
`

// runDoctorSecurity is orb doctor --security. It is a separate review
// rather than more lines in orb doctor's list: it answers a different
// question, it needs no database and no build, and a finding that matters
// should not be the twentieth line of a report about Docker.
func runDoctorSecurity(ctx context.Context, app appInfo, asJSON bool, stdout io.Writer) error {
	scan := scanApp(app.dir)
	res := securityResult{
		App:      filepath.Base(app.dir),
		Copies:   scan.copies(ctx, app),
		Findings: scan.rules(),
		Rules:    securityRules,
	}
	if res.Copies == nil {
		res.Copies = []securityCopy{}
	}
	res.Checked = securityChecked(scan, len(res.Copies))
	res.NotChecked = securityNotChecked
	for _, c := range res.Copies {
		res.countStatus(c.Status)
	}
	for _, f := range res.Findings {
		res.countStatus(f.Status)
	}

	if asJSON {
		if err := writeJSON(stdout, res); err != nil {
			return err
		}
	} else {
		res.report(stdout)
	}
	if res.Failures > 0 {
		return errDoctorFailed
	}
	return nil
}

func (r *securityResult) countStatus(status string) {
	switch status {
	case doctorFail:
		r.Failures++
	case doctorWarn:
		r.Warnings++
	}
}

// securityChecked says what the review looked at, in the app's own
// numbers.
func securityChecked(scan *securityScan, copies int) []string {
	return []string{
		fmt.Sprintf("%d modules gorbital.lock records as copied from the library, file by file, against the version the app requires now", copies),
		fmt.Sprintf("%d Go files under %s, and the %d migrations in %s, against the %d rules below", scan.scanGoFiles(), securityDirsPhrase(), scan.migrationCount(), filepath.ToSlash(migrationsDir), len(securityRules)),
	}
}

// securityDirsPhrase names the directories the rules read, in prose.
func securityDirsPhrase() string {
	dirs := make([]string, len(securityScanDirs))
	for i, d := range securityScanDirs {
		dirs[i] = d + "/"
	}
	return strings.Join(dirs, " and ")
}

// securityNotChecked is the end of every run, clean or not. It is part of
// the command, not a disclaimer bolted to it: a check people read as an
// assurance is worse than no check, and the only defence against that is
// to say plainly, every time, what was never looked at.
var securityNotChecked = []string{
	"whether any change upstream is a security fix. There is no advisory feed in gorbital, and nothing here invents one: no severities, no advisory IDs. The flow named beside a changed file is a guess from its name, not a verdict",
	"the library's own code, the modules it keeps (password hashing, session tokens, TOTP, API-key hashing), and everything else go.mod pulls in — govulncheck and your dependency scanner read those",
	"test files, generated API documents, SQL built at run time, and any table a migration this command could not read declares",
	"anything that needs the app running: configuration, TLS, what the database role may do, and whether row-level security is on (orb doctor reads the migrations' policies)",
	"your own rules. These seven are patterns for mistakes that have names; nothing matched is not the same as nothing wrong, and a clean run is not an assurance",
}

func (r securityResult) report(w io.Writer) {
	s := newStyles(w)
	fmt.Fprintf(w, "%s\n\n", s.strong.Render("orb doctor --security · "+r.App))

	fmt.Fprintf(w, "%s\n\n", s.strong.Render("The code copied from the library"))
	if len(r.Copies) == 0 {
		fmt.Fprintf(w, "  %s\n\n", s.muted.Render("gorbital.lock records no copied modules: this app has no sign-in or organisations of its own."))
	}
	for _, c := range r.Copies {
		r.reportCopy(w, s, c)
	}

	fmt.Fprintf(w, "%s\n\n", s.strong.Render("The app's own code"))
	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "  %s\n\n", s.muted.Render("No rule matched."))
	}
	for _, f := range r.Findings {
		status := fmt.Sprintf("%-4s", f.Status)
		if f.Status == doctorFail {
			status = s.accent.Render(status)
		}
		fmt.Fprintf(w, "  %s  %s:%d\n", status, f.File, f.Line)
		fmt.Fprintf(w, "        %s\n", f.Message)
		fmt.Fprintf(w, "        %s\n", s.dim.Render("why: "+f.Why))
		fmt.Fprintf(w, "        %s\n", s.dim.Render("fix: "+f.Fix))
		fmt.Fprintf(w, "        %s\n\n", s.dim.Render("read: docs/guides/"+f.Docs+" ("+f.Rule+")"))
	}

	fmt.Fprintf(w, "%s\n\n", s.strong.Render("What this checked"))
	for _, line := range r.Checked {
		writeWrapped(w, s.muted, "  ", "    ", line, 74)
	}
	for _, rule := range r.Rules {
		writeWrapped(w, s.muted, "    "+rule.Name+": ", "      ", rule.About, 70)
	}
	fmt.Fprintf(w, "\n%s\n\n", s.strong.Render("What this did not check"))
	for _, line := range r.NotChecked {
		writeWrapped(w, s.muted, "  - ", "    ", line, 72)
	}
	fmt.Fprintf(w, "\n  %d failed, %d warnings\n", r.Failures, r.Warnings)
}

// maxReportedFiles is how many changed files the report names before
// counting the rest; --json has them all.
const maxReportedFiles = 8

func (r securityResult) reportCopy(w io.Writer, s styles, c securityCopy) {
	status := fmt.Sprintf("%-4s", c.Status)
	switch c.Status {
	case doctorFail:
		status = s.accent.Render(status)
	case doctorOK:
		status = s.muted.Render(status)
	}
	fmt.Fprintf(w, "  %s  %s\n", status, s.ink.Render(c.Directory))
	line := fmt.Sprintf("copied from %s %s on %s", c.Package, c.Recorded, c.RecordedDate)
	if c.Current != "" {
		line += "; the app requires " + c.Current
	}
	fmt.Fprintf(w, "        %s\n", line)
	if c.Provenance != "" {
		fmt.Fprintf(w, "        %s\n", s.dim.Render("provenance: "+c.Provenance))
	}
	switch {
	case c.Held == 0:
	case len(c.ChangedUpstream) == 0:
		fmt.Fprintf(w, "        %s\n", "none of the "+quantity(c.Held, "file")+" the app holds changed upstream")
	default:
		both := 0
		for _, f := range c.ChangedUpstream {
			if f.Edited {
				both++
			}
		}
		line := fmt.Sprintf("%d of the %d files the app holds changed upstream", len(c.ChangedUpstream), c.Held)
		switch {
		case len(c.EditedLocally) == 0:
			line += ", and the app has edited none of them, so porting a change is a copy"
		default:
			line += fmt.Sprintf("; the app has itself changed %d of the files it holds, %d of those among them, so porting those is a merge", len(c.EditedLocally), both)
		}
		fmt.Fprintf(w, "        %s\n", line)
		for i, f := range c.ChangedUpstream {
			if i == maxReportedFiles {
				fmt.Fprintf(w, "          %s\n", s.dim.Render(fmt.Sprintf("and %d more (--json has them all)", len(c.ChangedUpstream)-i)))
				break
			}
			notes := []string{}
			if f.Area != "" {
				notes = append(notes, "touches "+f.Area+" — a hint from the file's name, not a verdict")
			}
			if f.Edited {
				notes = append(notes, "you have edited this file")
			}
			if f.Gone {
				notes = append(notes, "no longer in the library")
			}
			suffix := ""
			if len(notes) > 0 {
				suffix = "  (" + strings.Join(notes, "; ") + ")"
			}
			fmt.Fprintf(w, "          %s%s\n", f.Path, s.dim.Render(suffix))
		}
	}
	if n := len(c.AddedUpstream); n > 0 {
		fmt.Fprintf(w, "        %s\n", fmt.Sprintf("%d files the library has gained since, which the app has no copy of", n))
	}
	for _, e := range c.Changelog {
		heading := e.Version
		if e.Date != "" {
			heading += " (" + e.Date + ")"
		}
		fmt.Fprintf(w, "        %s\n", s.dim.Render("changelog "+heading+":"))
		for _, entry := range e.Entries {
			writeWrapped(w, s.dim, "          - ", "            ", entry, 70)
		}
	}
	for _, u := range c.Undetermined {
		writeWrapped(w, s.dim, "        not determined: ", "          ", u, 70)
	}
	if c.Fix != "" {
		fmt.Fprintf(w, "        %s\n", s.dim.Render("fix: "+c.Fix))
	}
	fmt.Fprintln(w)
}

// writeWrapped writes text broken at width, prefix before the first line
// and indent before the rest. Each line is styled on its own: a style
// applied to several lines at once pads them all to the longest, and the
// trailing spaces end up in anything that captures the output.
func writeWrapped(w io.Writer, style lipgloss.Style, prefix, indent, text string, width int) {
	column := len([]rune(prefix))
	fmt.Fprint(w, prefix)
	for i, word := range strings.Fields(text) {
		switch {
		case i == 0:
			column += len([]rune(word))
		case column+1+len([]rune(word)) > width:
			fmt.Fprint(w, "\n"+indent)
			column = len([]rune(indent)) + len([]rune(word))
		default:
			fmt.Fprint(w, " ")
			column += 1 + len([]rune(word))
			fmt.Fprint(w, style.Render(word))
			continue
		}
		fmt.Fprint(w, style.Render(word))
	}
	fmt.Fprintln(w)
}

// quantity is "1 file" or "3 files".
func quantity(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
