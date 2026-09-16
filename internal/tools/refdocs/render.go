package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// reference renders the pages from both golden apps' dumps.
type reference struct {
	multi, single dump
	desc          descriptions
}

func newReference(multi, single dump, desc descriptions) *reference {
	return &reference{multi: multi, single: single, desc: desc}
}

type page struct {
	file    string
	content []byte
}

func (r *reference) pages() []page {
	return []page{
		{"error-codes.md", r.errorCodes()},
		{"audit-actions.md", r.auditActions()},
		{"permissions.md", r.permissions()},
		{"settings.md", r.settings()},
		{"jobs.md", r.jobs()},
	}
}

// warnings lists names without a description, so they get one.
func (r *reference) warnings() []string {
	var out []string
	for _, c := range slices.Sorted(maps.Keys(r.codeGroups())) {
		if r.codeMeaning(c) == "" {
			out = append(out, fmt.Sprintf("error code %q has no detail in its mapping and no entry in descriptions.json", c))
		}
	}
	for _, a := range slices.Sorted(maps.Keys(r.actionGroups())) {
		if r.desc.AuditActions[a] == "" {
			out = append(out, fmt.Sprintf("audit action %q has no entry in descriptions.json", a))
		}
	}
	for _, j := range r.multi.Jobs {
		if j.Description == "" && r.desc.Jobs[j.Name] == "" {
			out = append(out, fmt.Sprintf("job %q has no description", j.Name))
		}
	}
	return out
}

const generatedNote = "<!-- Generated from examples/full-multi and examples/full-single by `go run -C internal/tools/refdocs . -write`. Don't edit: change the code, or internal/tools/refdocs/descriptions.json. -->\n\n"

// multiOnly is the note for names only multi-tenant apps have.
const (
	multiOnly  = "*Multi-tenant apps only.*"
	singleOnly = "*Single-tenant apps only.*"
)

func header(b *bytes.Buffer, title string, intro ...string) {
	fmt.Fprintf(b, "# %s\n\n", title)
	b.WriteString(generatedNote)
	for _, p := range intro {
		b.WriteString(p + "\n\n")
	}
}

// ---- Error codes ----

func (r *reference) codeGroups() map[string][]code {
	groups := map[string][]code{}
	for _, c := range r.multi.Codes {
		groups[c.Code] = append(groups[c.Code], c)
	}
	return groups
}

func (r *reference) codeMeaning(name string) string {
	if d := r.desc.ErrorCodes[name]; d != "" {
		return d
	}
	var details []string
	generic := 0
	for _, c := range r.codeGroups()[name] {
		if c.Generic {
			generic = c.Status
			continue
		}
		if c.Detail != "" && !slices.Contains(details, sentence(c.Detail)) {
			details = append(details, sentence(c.Detail))
		}
	}
	if len(details) == 0 && generic != 0 {
		return sentence(http.StatusText(generic))
	}
	return strings.Join(details, " ")
}

func (r *reference) errorCodes() []byte {
	var b bytes.Buffer
	header(&b, "Error codes",
		"Every error response is `application/problem+json` with a stable `code` clients can branch on; the `detail` text may change. Codes are public API: they are added, never renamed or removed ([Stability](../guides/stability.md)). How errors become responses, and how to add one: [Error handling](../guides/error-handling.md).",
		"These are the codes of a Full app as generated, including the example `projects` resource and the generic codes any status without its own code gets. Resources you add with `orb gen resource` add `<resource>_not_found`, `<resource>_version_conflict` and, for unique fields, `<resource>_<field>_taken`.",
	)
	single := map[string]bool{}
	for _, c := range r.single.Codes {
		single[c.Code] = true
	}
	groups := r.codeGroups()
	b.WriteString("| Code | HTTP status | Meaning | Where |\n|---|---|---|---|\n")
	for _, name := range slices.Sorted(maps.Keys(groups)) {
		meaning := r.codeMeaning(name)
		if !single[name] {
			meaning += " " + multiOnly
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", name, statuses(groups[name]), cell(meaning), cell(r.codeWhere(name, groups[name])))
	}
	return b.Bytes()
}

// statuses lists a code's statuses: those it is written with, then the
// statuses it is the generic code for.
func statuses(cs []code) string {
	var explicit, generic []int
	for _, c := range cs {
		switch {
		case c.Generic:
			generic = append(generic, c.Status)
		case c.Status != 0:
			explicit = append(explicit, c.Status)
		}
	}
	slices.Sort(explicit)
	explicit = slices.Compact(explicit)
	var parts []string
	for _, s := range explicit {
		parts = append(parts, strconv.Itoa(s))
	}
	switch {
	case len(generic) == 1 && !slices.Contains(explicit, generic[0]):
		parts = append(parts, strconv.Itoa(generic[0]))
	case len(generic) > 1:
		parts = append(parts, fmt.Sprintf("any other %dxx", generic[0]/100))
	}
	if len(parts) == 0 {
		return "varies"
	}
	return strings.Join(parts, ", ")
}

// areaLabels say where the codes of an area are returned. An area is an app
// module (internal/modules/<name>, internal/app/module_<name>.go); "app" is
// the app's own wiring and gorbital's middleware.
var areaLabels = map[string]string{
	"app":      "Any endpoint",
	"auth":     "`/v1/auth`",
	"ops":      "`/ops`",
	"orgs":     "`/v1/orgs`, `/v1/invitations`",
	"ping":     "`/v1/ping`, `/v1/echo`",
	"projects": "`/v1/orgs/{orgId}/projects` (`/v1/projects` in single-tenant apps)",
}

func (r *reference) codeWhere(name string, cs []code) string {
	if w := r.desc.ErrorCodeWhere[name]; w != "" {
		return w
	}
	// A generic code can come from any endpoint, such as Huma's validation.
	areas := map[string]bool{}
	for _, c := range cs {
		if c.Generic {
			areas["app"] = true
		} else {
			areas[area(c.Location)] = true
		}
	}
	if areas["app"] {
		return areaLabels["app"]
	}
	var labels []string
	for _, a := range slices.Sorted(maps.Keys(areas)) {
		label, ok := areaLabels[a]
		if !ok {
			label = "`" + a + "` module"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "; ")
}

// area returns the module a source location belongs to, or "app".
func area(location string) string {
	switch {
	case strings.HasPrefix(location, "internal/modules/"):
		name, _, _ := strings.Cut(strings.TrimPrefix(location, "internal/modules/"), "/")
		return name
	case strings.HasPrefix(location, "internal/app/module_"):
		return strings.TrimSuffix(strings.TrimPrefix(location, "internal/app/module_"), ".go")
	}
	return "app"
}

// ---- Audit actions ----

func (r *reference) actionGroups() map[string][]action {
	groups := map[string][]action{}
	for _, a := range r.multi.Actions {
		groups[a.Action] = append(groups[a.Action], a)
	}
	return groups
}

var actionSections = map[string]string{
	"auth":      "Authentication",
	"jobs":      "Background jobs",
	"mail":      "Email",
	"orgs":      "Organisations",
	"projects":  "Projects (example resource)",
	"retention": "Retention",
	"settings":  "Runtime settings",
}

func (r *reference) auditActions() []byte {
	var b bytes.Buffer
	header(&b, "Audit actions",
		"Security-relevant changes are recorded as audit events in PostgreSQL and read through `GET /ops/audit` ([Ops API](../guides/ops-api.md#audit-log)). Action names are public API: filters, alerts and dashboards depend on them, so they are added, never renamed or removed ([Stability](../guides/stability.md)).",
		"Every event has `occurred_at`, `actor_kind` (`user`, `service`, `system` for jobs and commands, or `anonymous`) and `actor_id`, `action`, `outcome`, `resource_type` and `resource_id`, `org_id` in multi-tenant apps, `request_id`, `trace_id`, `ip` and `user_agent` when there is a request, and `metadata`. The metadata keys below are those the code sets; the recorder redacts sensitive keys and bounds the size, and events never hold passwords, tokens or secrets.",
	)
	single := map[string]bool{}
	for _, a := range r.single.Actions {
		single[a.Action] = true
	}
	groups := r.actionGroups()
	bySection := map[string][]string{}
	for name := range groups {
		prefix, _, _ := strings.Cut(name, ".")
		bySection[prefix] = append(bySection[prefix], name)
	}
	for _, prefix := range slices.Sorted(maps.Keys(bySection)) {
		names := slices.Sorted(slices.Values(bySection[prefix]))
		title, ok := actionSections[prefix]
		if !ok {
			title = prefix
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		allMulti := !slices.ContainsFunc(names, func(n string) bool { return single[n] })
		if allMulti {
			b.WriteString(multiOnly + "\n\n")
		}
		b.WriteString("| Action | Recorded when | Metadata |\n|---|---|---|\n")
		for _, name := range names {
			var keys []string
			for _, a := range groups[name] {
				keys = append(keys, a.Metadata...)
			}
			slices.Sort(keys)
			keys = slices.Compact(keys)
			when := r.desc.AuditActions[name]
			if !single[name] && !allMulti {
				when += " " + multiOnly
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", name, cell(when), codeList(keys))
		}
		b.WriteString("\n")
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// ---- Permissions and roles ----

var catalogIntros = map[string][2]string{
	"platform": {"Platform", "Platform roles are held across the whole app; the ops roles grant access to `/ops`. Every user holds the `user` role without a grant: it covers what they do with their own data and outside organisation roles, so an API key's scopes limit that too ([API keys](../guides/api-keys.md)). Give and take the other roles with `go run ./cmd/api grant-role <email> <role>` and `revoke-role`; list them with `go run ./cmd/api roles`. `orb gen resource --scope user` adds `<resource>.<resource>.read` and `.write` permissions, granted to the `user` role."},
	"org":      {"Organisation", "Every member of an organisation has exactly one of these roles in it, and it grants permissions only in that organisation. Platform roles never grant them. `orb gen resource --scope org` adds `<resource>.<resource>.read` and `.write` permissions, granted to every role."},
}

func (r *reference) permissions() []byte {
	var b bytes.Buffer
	header(&b, "Permissions and roles",
		"Access is denied by default: a user holds only the permissions of their roles. Permission and role names are public API ([Stability](../guides/stability.md)); they are declared in `internal/app/permissions.go`. How checks work: [Authentication](../guides/authentication.md).",
		"A role that requires two-factor authentication grants its permissions only to sessions signed in with a second factor; other sessions get `403 mfa_required`.",
	)
	single := map[string]catalog{}
	for _, c := range r.single.Catalogs {
		single[c.Name] = c
	}
	catalogs := slices.Clone(r.multi.Catalogs)
	order := func(name string) int {
		if i := slices.Index([]string{"platform", "org"}, name); i >= 0 {
			return i
		}
		return 2
	}
	slices.SortFunc(catalogs, func(a, b catalog) int {
		if c := order(a.Name) - order(b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, c := range catalogs {
		sc, inSingle := single[c.Name]
		var marks map[string]string
		if inSingle {
			c, marks = mergeCatalogs(c, sc)
		}
		title, intro := c.Name, ""
		if known, ok := catalogIntros[c.Name]; ok {
			title, intro = known[0], known[1]
		}
		fmt.Fprintf(&b, "## %s roles\n\n", title)
		if !inSingle {
			b.WriteString(multiOnly + " ")
		}
		if intro != "" {
			b.WriteString(intro)
		}
		b.WriteString("\n\n| Role | Description | Requires two-factor authentication |\n|---|---|---|\n")
		for _, role := range c.Roles {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", role.Name, cell(sentence(role.Description)), yesNo(role.RequiresMFA))
		}
		fmt.Fprintf(&b, "\n## %s permissions\n\n", title)
		b.WriteString("| Permission | Description |")
		for _, role := range c.Roles {
			fmt.Fprintf(&b, " `%s` |", role.Name)
		}
		b.WriteString("\n|---|---|" + strings.Repeat("---|", len(c.Roles)) + "\n")
		for _, p := range c.Permissions {
			desc := sentence(p.Description)
			if mark := marks[p.Name]; mark != "" {
				desc += " " + mark
			}
			fmt.Fprintf(&b, "| `%s` | %s |", p.Name, cell(desc))
			for _, role := range c.Roles {
				if slices.Contains(role.Permissions, p.Name) {
					b.WriteString(" yes |")
				} else {
					b.WriteString(" |")
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// mergeCatalogs returns multi's catalog with the permissions and role grants
// only single's has, and marks for permissions only one of them declares.
// The same resource can be user-scoped in one app and org-scoped in the
// other, as the example projects are.
func mergeCatalogs(multi, single catalog) (catalog, map[string]string) {
	marks := map[string]string{}
	has := func(c catalog, name string) bool {
		return slices.ContainsFunc(c.Permissions, func(p permission) bool { return p.Name == name })
	}
	for _, p := range multi.Permissions {
		if !has(single, p.Name) {
			marks[p.Name] = multiOnly
		}
	}
	merged := catalog{Name: multi.Name, Permissions: slices.Clone(multi.Permissions)}
	for _, p := range single.Permissions {
		if !has(multi, p.Name) {
			merged.Permissions = append(merged.Permissions, p)
			marks[p.Name] = singleOnly
		}
	}
	for _, r := range multi.Roles {
		r.Permissions = slices.Clone(r.Permissions)
		if i := slices.IndexFunc(single.Roles, func(sr role) bool { return sr.Name == r.Name }); i >= 0 {
			for _, p := range single.Roles[i].Permissions {
				if !slices.Contains(r.Permissions, p) {
					r.Permissions = append(r.Permissions, p)
				}
			}
		}
		merged.Roles = append(merged.Roles, r)
	}
	return merged, marks
}

// ---- Settings ----

var settingGroups = map[string]string{
	"auth":        "Authentication",
	"example":     "Example",
	"mail":        "Email",
	"maintenance": "Maintenance mode",
	"orgs":        "Organisations",
	"rate_limits": "Rate limits",
	"retention":   "Retention",
}

func (r *reference) settings() []byte {
	var b bytes.Buffer
	header(&b, "Runtime settings",
		"Runtime settings are non-secret values operators change without a redeploy: `PUT /ops/settings/{key}` stores the value in PostgreSQL and every instance applies it within seconds ([Runtime settings](../guides/runtime-settings.md), [Ops API](../guides/ops-api.md)). Secrets and infrastructure are environment variables instead ([Environment variables](../guides/environment-variables.md)). Setting keys are public API: stored values and history refer to them ([Stability](../guides/stability.md)).",
		"**Reason required**: a change must say why (`reason`), and the reason is kept in the history and the audit event. **Restart required**: the value takes effect when an instance starts. Durations are written as Go durations in the API, such as `\"720h\"`.",
	)
	single := map[string]bool{}
	for _, s := range r.single.Settings {
		single[s.Key] = true
	}
	var groups []string
	byGroup := map[string][]setting{}
	for _, s := range r.multi.Settings {
		if _, ok := byGroup[s.Group]; !ok {
			groups = append(groups, s.Group)
		}
		byGroup[s.Group] = append(byGroup[s.Group], s)
	}
	for _, g := range groups {
		title, ok := settingGroups[g]
		if !ok {
			title = g
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		allMulti := !slices.ContainsFunc(byGroup[g], func(s setting) bool { return single[s.Key] })
		if allMulti {
			b.WriteString(multiOnly + "\n\n")
		}
		b.WriteString("| Key | Type | Default | Allowed | Reason required | Restart required | Description |\n|---|---|---|---|---|---|---|\n")
		for _, s := range byGroup[g] {
			description := s.Description
			if !single[s.Key] && !allMulti {
				description += " " + multiOnly
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n", s.Key, s.Kind, r.settingDefault(s), cell(constraints(s.Constraints)), yesNo(s.ReasonRequired), yesNo(s.RestartRequired), cell(description))
		}
		b.WriteString("\n")
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

func (r *reference) settingDefault(s setting) string {
	var v any
	if err := json.Unmarshal(s.Default, &v); err != nil {
		return "`" + string(s.Default) + "`"
	}
	if str, ok := v.(string); ok {
		switch {
		case s.Kind == "duration":
			return humanDuration(str)
		case str == "":
			return "empty"
		case str == r.multi.ServiceName:
			return "the app's name"
		}
	}
	return "`" + cell(string(s.Default)) + "`"
}

// constraints describes a setting's validation summary.
func constraints(c map[string]any) string {
	var parts []string
	value := func(v any) string {
		if s, ok := v.(string); ok {
			if _, err := time.ParseDuration(s); err == nil {
				return humanDuration(s)
			}
			return s
		}
		return fmt.Sprint(v)
	}
	minV, hasMin := c["min"]
	maxV, hasMax := c["max"]
	switch {
	case hasMin && hasMax:
		parts = append(parts, value(minV)+" to "+value(maxV))
	case hasMin:
		parts = append(parts, "at least "+value(minV))
	case hasMax:
		parts = append(parts, "at most "+value(maxV))
	}
	for _, k := range slices.Sorted(maps.Keys(c)) {
		v := c[k]
		switch k {
		case "min", "max":
		case "max_len":
			parts = append(parts, fmt.Sprintf("at most %v characters", v))
		case "max_items":
			parts = append(parts, fmt.Sprintf("at most %v items", v))
		case "format":
			if v == "email" {
				parts = append(parts, "an email address")
			} else {
				parts = append(parts, fmt.Sprintf("format %v", v))
			}
		case "one_of":
			var names []string
			if list, ok := v.([]any); ok {
				for _, n := range list {
					names = append(names, fmt.Sprintf("`%v`", n))
				}
			}
			parts = append(parts, "one of "+strings.Join(names, ", "))
		default:
			parts = append(parts, fmt.Sprintf("%s %v", k, v))
		}
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, "; ")
}

// humanDuration writes a Go duration in the largest whole unit, such as
// "30 days" for 720h0m0s.
func humanDuration(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		return "`" + s + "`"
	}
	day := 24 * time.Hour
	for _, u := range []struct {
		size time.Duration
		name string
	}{{365 * day, "year"}, {day, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if d >= u.size && d%u.size == 0 {
			n := int64(d / u.size)
			if n == 1 {
				return "1 " + u.name
			}
			return fmt.Sprintf("%d %ss", n, u.name)
		}
	}
	return d.String()
}

// ---- Jobs ----

func (r *reference) jobs() []byte {
	var b bytes.Buffer
	header(&b, "Jobs",
		"Background jobs run on River in PostgreSQL ([Background jobs](../guides/background-jobs.md)). Each job definition's schedule, whether it is enabled, its timeout, attempts, queue and priority can be changed at runtime through `/ops/jobs/definitions/{name}` ([Ops API](../guides/ops-api.md)); the values below are the defaults in code. Schedules are evaluated in UTC. Job names are public API: overrides, history and queued jobs refer to them ([Stability](../guides/stability.md)).",
	)
	single := map[string]bool{}
	for _, j := range r.single.Jobs {
		single[j.Name] = true
	}
	b.WriteString("| Job | Default schedule | Enabled | Timeout | Attempts | Queue | Priority | What it does |\n|---|---|---|---|---|---|---|---|\n")
	for _, j := range r.multi.Jobs {
		description := j.Description
		if d := r.desc.Jobs[j.Name]; d != "" {
			description = d
		}
		if !single[j.Name] {
			description += " " + multiOnly
		}
		if !j.Definition {
			fmt.Fprintf(&b, "| `%s` | none | always | | | | | %s |\n", j.Name, cell(sentence(description)))
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %d | `%s` | %d | %s |\n", j.Name, schedule(j.Schedule), yesNo(j.Enabled), humanDuration(j.Timeout), j.MaxAttempts, j.Queue, j.Priority, cell(sentence(description)))
	}
	return b.Bytes()
}

// schedule writes a cron schedule with its meaning when it is a simple one.
func schedule(s string) string {
	if s == "" {
		return "on demand"
	}
	if every, ok := strings.CutPrefix(s, "@every "); ok {
		if d, err := time.ParseDuration(every); err == nil {
			h := humanDuration(d.String())
			return fmt.Sprintf("`%s` (every %s)", s, strings.TrimPrefix(h, "1 "))
		}
	}
	f := strings.Fields(s)
	if len(f) == 5 && f[2] == "*" && f[3] == "*" && f[4] == "*" {
		minute, errM := strconv.Atoi(f[0])
		hour, errH := strconv.Atoi(f[1])
		switch {
		case errM == nil && errH == nil:
			return fmt.Sprintf("`%s` (daily at %02d:%02d)", s, hour, minute)
		case errM == nil && f[1] == "*":
			return fmt.Sprintf("`%s` (hourly at minute %d)", s, minute)
		}
	}
	return "`" + s + "`"
}

// ---- Formatting ----

// sentence capitalises s and ends it with a full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// An identifier such as return_to keeps its case.
	if first, _, _ := strings.Cut(s, " "); !strings.ContainsAny(first, "_.") {
		r, size := utf8.DecodeRuneInString(s)
		s = string(unicode.ToUpper(r)) + s[size:]
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ".*") {
		s += "."
	}
	return s
}

// cell makes s safe inside a Markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "|", `\|`), "\n", " ")
}

func codeList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "`" + strings.Join(names, "`, `") + "`"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
