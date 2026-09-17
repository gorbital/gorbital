package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/recipes"
)

var (
	jobDefinePattern = regexp.MustCompile(`func define(\w+)Job\(`)
	jobImportPattern = regexp.MustCompile(`"[^"\n]+/internal/jobs/([a-z0-9_]+)"`)
)

// jobSources lists the jobs in an app for the portal (ADR-0071): in the v0.1
// layout from their definition files, internal/app/job_<name>.go, and in the
// v0.2 layout from the modules that declare them (ADR-0083). A v0.1
// definition with an //orb:job marker was generated; when its worker file
// still hashes to the marker's value, the portal edits it as a form. A
// module's jobs are the app's own code, so they are listed as custom jobs
// with the file that defines them; the Jobs screen's runtime data comes from
// /ops/jobs either way.
func jobSources(dir string) ([]portal.JobSource, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), "internal/app")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return moduleJobSources(root)
		}
		return nil, err
	}
	jobs := []portal.JobSource{}
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), "job_")
		name, ok2 := strings.CutSuffix(name, ".go")
		if e.IsDir() || !ok || !ok2 || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		defPath := "internal/app/" + e.Name()
		src, err := root.ReadFile(defPath)
		if err != nil {
			return nil, err
		}
		job := portal.JobSource{Name: name, Definition: defPath, Kind: recipes.KindCustom}
		if m := jobDefinePattern.FindSubmatch(src); m != nil {
			job.Ident = string(m[1])
		}
		if m := jobImportPattern.FindSubmatch(src); m != nil {
			job.Package = string(m[1])
			job.Worker = fmt.Sprintf("internal/jobs/%s/%s.go", job.Package, job.Package)
		}
		if marker, ok := recipes.ParseJobMarker(src); ok {
			job.Generated = true
			worker, err := root.ReadFile(job.Worker)
			job.Ejected = job.Worker == "" || err != nil || recipes.WorkerHash(worker) != marker.Worker
			if !job.Ejected {
				job.Kind = marker.Kind
				job.Form, _ = json.Marshal(marker)
			}
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return jobs, nil
}

// moduleJobDefine matches a module's job definition: jobs.Define with a
// jobs.Definition literal whose Name is a string.
var moduleJobDefine = regexp.MustCompile(`jobs\.Define\(\s*\w+,\s*jobs\.Definition\[[^\]]*\]\{(?s).*?Name:\s*"([^"\n]+)"`)

// moduleJobSources lists the jobs the app's modules declare in
// internal/modules, for apps on gorbital.Main. A job whose Name isn't a
// literal in the same call isn't listed: the portal would show a name the
// app doesn't use. The runtime list (/ops/jobs) always has every job.
func moduleJobSources(root *os.Root) ([]portal.JobSource, error) {
	jobs := []portal.JobSource{}
	err := fs.WalkDir(root.FS(), "internal/modules", func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return fs.SkipAll
		case err != nil:
			return err
		case d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
			return nil
		}
		src, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		module := strings.SplitN(strings.TrimPrefix(p, "internal/modules/"), "/", 2)[0]
		for _, m := range moduleJobDefine.FindAllSubmatch(src, -1) {
			jobs = append(jobs, portal.JobSource{
				Name: string(m[1]), Package: module, Definition: p, Worker: p, Kind: recipes.KindCustom,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return jobs, nil
}
