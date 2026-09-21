package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/recipes"
)

// profile checks the app against the profile it records: the manifest of
// the templates it was written from against the files on disk, and
// cmd/api/main.go against gorbital.yaml (ADR-0090 §8). Both catch the same
// kind of mistake from opposite ends — an app that says it is one shape
// and is another.
func (d *doctor) profile() {
	profile, ok := d.profileFromLock()
	if !ok {
		return
	}
	d.profileFiles(profile)
	d.profileWiring(profile)
}

// profileFromLock returns the profile gorbital.lock records, and false for
// an app with no lock or one written before v0.3.0, which records no
// profile to check.
func (d *doctor) profileFromLock() (recipes.Profile, bool) {
	lock, err := readLock(d.dir)
	if err != nil || lock.Inputs.layout() != recipes.LayoutV02 || lock.Inputs.Auth == "" {
		return recipes.Profile{}, false
	}
	profile, err := lock.Inputs.profile()
	if err != nil {
		d.add(doctorFail, "profile", err.Error(), "fix the auth and scope in "+lockPath)
		return recipes.Profile{}, false
	}
	return profile, true
}

// profileFiles reports a file the profile's manifest says the app should
// have and hasn't, and one it says the app shouldn't have and has. A file
// the developer added is not one of those: only the paths the templates
// own are checked.
func (d *doctor) profileFiles(profile recipes.Profile) {
	paths, err := recipes.ManifestPaths(recipes.Templates(), recipes.TreeV03)
	if err != nil {
		d.add(doctorFail, "profile", err.Error(), "")
		return
	}
	features := profile.Features()
	var missing, extra []string
	for path, needed := range paths {
		rel := strings.TrimSuffix(path, ".tmpl")
		_, err := os.Stat(d.path(rel))
		switch want := recipes.Wants(features, needed); {
		case want && err != nil:
			missing = append(missing, rel)
		case !want && err == nil:
			extra = append(extra, rel)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	switch {
	case len(missing) > 0:
		d.add(doctorWarn, "profile files", fmt.Sprintf("%s says %s, and %s %s missing", lockPath, profile, strings.Join(missing, ", "), were(len(missing))),
			"restore them with git, or record the shape the app really is in "+lockPath)
	case len(extra) > 0:
		d.add(doctorWarn, "profile files", fmt.Sprintf("%s says %s, and %s %s here all the same", lockPath, profile, strings.Join(extra, ", "), were(len(extra))),
			"delete them, or record the shape the app really is in "+lockPath)
	default:
		d.add(doctorOK, "profile files", fmt.Sprintf("every file %s asks for is here, and no file it doesn't", profile), "")
	}
}

// were agrees the verb with the number of paths.
func were(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// profileWiring reports a cmd/api/main.go that doesn't build what
// gorbital.yaml says the app is: sign-in it declares and doesn't mount, or
// a tenancy it declares and doesn't give the app.
func (d *doctor) profileWiring(profile recipes.Profile) {
	main, err := os.ReadFile(filepath.Join(d.dir, filepath.FromSlash("cmd/api/main.go"))) //nolint:gosec // the app's own main.go
	if err != nil {
		return
	}
	declared := recipes.ProfileFromManifest(manifestSource(d.dir))
	source := string(main)
	var wrong []string
	if declared.Auth != "" && declared.Auth != profile.Auth {
		wrong = append(wrong, fmt.Sprintf("gorbital.yaml says auth: %s and %s says %s", declared.Auth, lockPath, profile.Auth))
	}
	if declared.Scope != "" && declared.Scope != profile.Scope {
		wrong = append(wrong, fmt.Sprintf("gorbital.yaml says scope: %s and %s says %s", declared.Scope, lockPath, profile.Scope))
	}
	switch {
	case profile.SignsIn() && !strings.Contains(source, "gorbital.WithAuth("):
		wrong = append(wrong, "auth: "+profile.Auth+", and main.go has no gorbital.WithAuth")
	case !profile.SignsIn() && strings.Contains(source, "gorbital.WithAuth("):
		wrong = append(wrong, "auth: none, and main.go calls gorbital.WithAuth")
	}
	switch {
	case profile.Named() && !strings.Contains(source, "orgshttp.Module("):
		wrong = append(wrong, "scope: "+profile.Scope+", and main.go doesn't mount orgshttp.Module")
	case profile.CustomScope() && !strings.Contains(source, "gorbital.WithScope("):
		wrong = append(wrong, "scope: custom, and main.go doesn't give the app a scope with gorbital.WithScope")
	case !profile.Scoped() && strings.Contains(source, "orgshttp.Module("):
		wrong = append(wrong, "scope: "+profile.Scope+", and main.go mounts orgshttp.Module")
	}
	if len(wrong) > 0 {
		d.add(doctorWarn, "profile wiring", strings.Join(wrong, "; "),
			"wire the app the way gorbital.yaml describes it, or describe the app the way it is wired")
		return
	}
	d.add(doctorOK, "profile wiring", "cmd/api/main.go builds the "+profile.String()+" app gorbital.yaml describes", "")
}
