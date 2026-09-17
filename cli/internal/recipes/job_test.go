package recipes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestJobMatchesGoldenApp checks that generating the heartbeat job with its
// defaults reproduces examples/v0.1/full-single exactly (ADR-0021, ADR-0035).
func TestJobMatchesGoldenApp(t *testing.T) {
	files, err := RenderJob(JobData{
		Module:      "example.com/acme-api",
		Ident:       "Heartbeat",
		Name:        "heartbeat",
		Package:     "heartbeat",
		Description: "Logs a heartbeat. An example job: change its schedule in /ops/jobs.",
		Enabled:     true,
		Schedule:    "@every 1h",
		Timeout:     time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    1,
	})
	if err != nil {
		t.Fatalf("RenderJob() error = %v", err)
	}
	golden := filepath.Join("..", "..", "..", "examples", "v0.1", "full-single")
	for _, f := range files {
		want, err := os.ReadFile(filepath.Join(golden, f.Path))
		if err != nil {
			t.Fatalf("read golden %s: %v", f.Path, err)
		}
		if string(f.Content) != string(want) {
			t.Errorf("generated %s differs from examples/v0.1/full-single:\n--- generated\n%s\n--- golden\n%s", f.Path, f.Content, want)
		}
	}
}

func TestTimeoutExpr(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Second:             "time.Second",
		30 * time.Second:        "30 * time.Second",
		time.Minute:             "time.Minute",
		90 * time.Second:        "90 * time.Second",
		5 * time.Minute:         "5 * time.Minute",
		2 * time.Hour:           "2 * time.Hour",
		1500 * time.Millisecond: "1500 * time.Millisecond",
	} {
		if got := (JobData{Timeout: d}).TimeoutExpr(); got != want {
			t.Errorf("TimeoutExpr(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRenderJobEscapesDescription(t *testing.T) {
	files, err := RenderJob(JobData{
		Module: "example.com/x", Ident: "Report", Name: "report", Package: "report",
		Description: `Sends "weekly" report\nnow`, Timeout: time.Minute, MaxAttempts: 1, Queue: "default", Priority: 1,
	})
	if err != nil {
		t.Fatalf("RenderJob() error = %v", err)
	}
	if def := string(files[2].Content); !strings.Contains(def, `Description: "Sends \"weekly\" report\\nnow"`) {
		t.Errorf("definition file doesn't quote the description safely:\n%s", def)
	}
}

func TestInsertAfterAnchor(t *testing.T) {
	src := []byte("package app\n\nfunc defineJobs() {\n\t//orb:anchor jobs\n\tdefineHeartbeatJob()\n}\n")

	got, err := InsertAfterAnchor(src, JobAnchor, "defineReportJob()")
	if err != nil {
		t.Fatalf("InsertAfterAnchor() error = %v", err)
	}
	want := "package app\n\nfunc defineJobs() {\n\t//orb:anchor jobs\n\tdefineReportJob()\n\tdefineHeartbeatJob()\n}\n"
	if string(got) != want {
		t.Errorf("InsertAfterAnchor() =\n%s\nwant\n%s", got, want)
	}

	if _, err := InsertAfterAnchor(got, JobAnchor, "defineReportJob()"); err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("second insert error = %v, want already present", err)
	}
	if _, err := InsertAfterAnchor([]byte("package app\n"), JobAnchor, "x()"); !errors.Is(err, ErrAnchorMissing) {
		t.Errorf("missing anchor error = %v, want ErrAnchorMissing", err)
	}
}
