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

// jobSources lists the jobs in an app from their definition files,
// internal/app/job_<name>.go, for the portal (ADR-0071). A definition
// with an //orb:job marker was generated; when its worker file still
// hashes to the marker's value, the portal edits it as a form.
func jobSources(dir string) ([]portal.JobSource, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), "internal/app")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []portal.JobSource{}, nil
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
