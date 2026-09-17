package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStorageApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "internal", "app", "storage.go"), "package app\n")
	writeFile(t, filepath.Join(dir, ".env.example"), "APP_ADDR=127.0.0.1:8080\n\n# orb:begin storage\nSTORAGE_DRIVER=\nSTORAGE_LOCAL_DIR=.orb/storage\n# orb:end storage\n\nGRAFANA_PORT=3000\n")
	writeFile(t, filepath.Join(dir, ".env"), "APP_ADDR=127.0.0.1:8080\n")
	writeFile(t, filepath.Join(dir, "compose.yaml"), "services:\n  postgres:\n    image: postgres:18\n\nvolumes:\n  postgres-data:\n")
	t.Chdir(dir)
	return dir
}

func TestAddStorageMinIO(t *testing.T) {
	dir := newStorageApp(t)
	code, out, errOut := runOrb(t, "add", "storage", "--driver", "minio", "--allow-dirty", "--json")
	if code != 0 || !strings.Contains(out, `"driver": "minio"`) || !strings.Contains(out, `"compose.yaml"`) {
		t.Fatalf("orb add storage = %d %q %q", code, out, errOut)
	}
	example := readFile(t, ".env.example")
	for _, want := range []string{"STORAGE_DRIVER=minio", "STORAGE_ENDPOINT=127.0.0.1:9000", "STORAGE_ACCESS_KEY=minioadmin", "MINIO_PORT=9000", "# orb:end storage", "GRAFANA_PORT=3000", "APP_ADDR=127.0.0.1:8080"} {
		if !strings.Contains(example, want) {
			t.Errorf(".env.example lacks %q:\n%s", want, example)
		}
	}
	env := readFile(t, ".env")
	for _, want := range []string{"STORAGE_DRIVER=minio", "STORAGE_SECRET_KEY=minioadmin", "STORAGE_BUCKET=" + filepath.Base(dir)} {
		if !strings.Contains(env, want) {
			t.Errorf(".env lacks %q:\n%s", want, env)
		}
	}
	compose := readFile(t, "compose.yaml")
	if !strings.Contains(compose, "\n  minio:\n") || !strings.Contains(compose, "minio-data:/data") || !strings.HasSuffix(compose, "volumes:\n  postgres-data:\n  minio-data:\n") || strings.Index(compose, "minio:") > strings.Index(compose, "\nvolumes:") {
		t.Errorf("compose.yaml:\n%s", compose)
	}
	// Running it again changes nothing more.
	if code, _, errOut := runOrb(t, "add", "storage", "--driver", "minio", "--allow-dirty", "--json"); code != 0 || strings.Count(readFile(t, "compose.yaml"), "  minio:") != 1 {
		t.Errorf("second run = %d %q, compose has %d minio services", code, errOut, strings.Count(readFile(t, "compose.yaml"), "  minio:"))
	}
}

func TestAddStorageS3AndValidation(t *testing.T) {
	newStorageApp(t)
	if code, out, _ := runOrb(t, "add", "storage", "--driver", "s3", "--region", "eu-west-1", "--bucket", "shop-files", "--access-key", "AKIA1", "--allow-dirty", "--yes"); code != 0 || !strings.Contains(out, "STORAGE_SECRET_KEY") {
		t.Fatalf("s3 = %d %q", code, out)
	}
	env := readFile(t, ".env")
	if !strings.Contains(env, "STORAGE_DRIVER=s3") || !strings.Contains(env, "STORAGE_REGION=eu-west-1") || !strings.Contains(env, "STORAGE_BUCKET=shop-files") || strings.Contains(env, "STORAGE_SECRET_KEY") {
		t.Errorf(".env = %s", env)
	}
	if code, _, errOut := runOrb(t, "add", "storage", "--driver", "gcs"); code != 2 || !strings.Contains(errOut, "--driver must be one of") {
		t.Errorf("bad driver = %d %q", code, errOut)
	}
	if code, _, errOut := runOrb(t, "add", "storage", "--driver", "r2", "--no-input"); code != 2 || !strings.Contains(errOut, "--bucket is required") {
		t.Errorf("no bucket = %d %q", code, errOut)
	}
	if code, out, _ := runOrb(t, "add", "storage", "--dry-run", "--yes"); code != 0 || !strings.Contains(out, "Would set up file storage with local") {
		t.Errorf("dry run = %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join("internal", "app", "storage.go")); err != nil {
		t.Fatal(err)
	}
}

func TestAddStorageOutsideFullPreset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n")
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, "add", "storage", "--driver", "local"); code == 0 || !strings.Contains(errOut, "Full preset") {
		t.Errorf("outside a Full app = %d %q", code, errOut)
	}
}
