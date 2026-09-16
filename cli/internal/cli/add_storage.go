package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/recipes"
)

// Storage drivers orb add storage knows (ADR-0075).
var storageDrivers = []string{"local", "s3", "spaces", "r2", "minio"}

type storageInput struct {
	driver, endpoint, region, bucket, accessKey, publicURL string
}

type addStorageResult struct {
	Driver       string   `json:"driver"`
	Files        []string `json:"files"`
	EnvVariables []string `json:"env_variables"`
	DryRun       bool     `json:"dry_run"`
}

// minioService is the compose service orb add storage --driver minio adds.
const minioService = `
  # MinIO: an S3-compatible object store for development (orb add storage
  # --driver minio, ADR-0075). Console at http://127.0.0.1:${MINIO_CONSOLE_PORT:-9001}.
  minio:
    image: minio/minio:latest
    command: ["server", "/data", "--console-address", ":9001"]
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports:
      - "127.0.0.1:${MINIO_PORT:-9000}:9000"
      - "127.0.0.1:${MINIO_CONSOLE_PORT:-9001}:9001"
    volumes:
      - minio-data:/data
    healthcheck:
      test: ["CMD", "mc", "ready", "local"]
      interval: 2s
      timeout: 3s
      retries: 30
`

func runAddStorage(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb add storage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var in storageInput
	flags.StringVar(&in.driver, "driver", "", "local, s3, spaces, r2 or minio")
	flags.StringVar(&in.endpoint, "endpoint", "", "the service's host (s3 and spaces derive it from --region; minio defaults to 127.0.0.1:9000)")
	flags.StringVar(&in.region, "region", "", "the region, such as us-east-1 or nyc3")
	flags.StringVar(&in.bucket, "bucket", "", "the bucket name")
	flags.StringVar(&in.accessKey, "access-key", "", "the access key; put STORAGE_SECRET_KEY in .env yourself or answer the prompt")
	flags.StringVar(&in.publicURL, "public-url", "", "where objects are reachable when the bucket is public")
	dryRun := flags.Bool("dry-run", false, "show what would change without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, addUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return usageError(fmt.Sprintf("unexpected argument %q: orb add storage takes only flags", args[0]))
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(app.dir, "internal", "app", "storage.go")); err != nil {
		return fmt.Errorf("%s has no internal/app/storage.go: orb add storage works in apps created with the Full preset (orb upgrade adds it)", app.dir)
	}
	example, err := os.ReadFile(filepath.Join(app.dir, envExamplePath))
	if err != nil {
		return fmt.Errorf("orb add storage needs %s: %w", envExamplePath, err)
	}
	if _, err := recipes.Block(example, "storage"); err != nil {
		return fmt.Errorf("%s has no storage block; add these two lines where the storage variables should go, then run orb add storage again:\n  # orb:begin storage\n  # orb:end storage", envExamplePath)
	}
	if in.driver == "" {
		in.driver = "local"
		if shouldPrompt(p, *asJSON, stdin, stdout) {
			fmt.Fprintln(stderr, "No --driver given: keeping files on this machine (local). Pass --driver s3, spaces, r2 or minio for a service.")
		}
	}
	if !contains(storageDrivers, in.driver) {
		return usageError(fmt.Sprintf("--driver must be one of %s, got %q", strings.Join(storageDrivers, ", "), in.driver))
	}
	if in.driver == "minio" {
		if in.endpoint == "" {
			in.endpoint = "127.0.0.1:9000"
		}
		if in.accessKey == "" {
			in.accessKey = "minioadmin"
		}
		if in.bucket == "" {
			in.bucket = filepath.Base(app.dir)
		}
	}
	if in.driver != "local" && in.driver != "minio" && in.bucket == "" && p.noInput {
		return usageError("--bucket is required with --no-input")
	}

	// What changes: the block in .env.example, the values in .env, and
	// the MinIO service.
	block := storageBlock(in)
	updatedExample, err := recipes.ReplaceBlock(example, "storage", []byte(block))
	if err != nil {
		return err
	}
	var files, envVars []string
	if string(updatedExample) != string(example) {
		files = append(files, envExamplePath)
	}
	set := map[string]string{"STORAGE_DRIVER": in.driver}
	for k, v := range map[string]string{"STORAGE_ENDPOINT": in.endpoint, "STORAGE_REGION": in.region, "STORAGE_BUCKET": in.bucket, "STORAGE_ACCESS_KEY": in.accessKey, "STORAGE_PUBLIC_URL": in.publicURL} {
		if v != "" {
			set[k] = v
		}
	}
	if in.driver == "minio" {
		set["STORAGE_SECRET_KEY"] = "minioadmin"
	}
	for k := range set {
		envVars = append(envVars, k)
	}
	slices.Sort(envVars)
	envPath := filepath.Join(app.dir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		files = append(files, ".env")
	}
	composePath := filepath.Join(app.dir, "compose.yaml")
	addMinio := false
	if in.driver == "minio" {
		compose, err := os.ReadFile(composePath)
		if err == nil && !strings.Contains(string(compose), "\n  minio:") {
			addMinio = true
			files = append(files, "compose.yaml")
		}
	}
	result := addStorageResult{Driver: in.driver, Files: files, EnvVariables: envVars, DryRun: *dryRun}
	if !*dryRun {
		if !*allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(app.dir, envExamplePath), updatedExample, 0o644); err != nil { //nolint:gosec // the example is committed
			return err
		}
		if data, err := os.ReadFile(envPath); err == nil {
			f := portal.ParseEnv(data)
			keys := make([]string, 0, len(set))
			for k := range set {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				f.Set(k, set[k], "")
			}
			if err := os.WriteFile(envPath, f.Bytes(), 0o600); err != nil {
				return err
			}
		}
		if addMinio {
			compose, _ := os.ReadFile(composePath)
			updated, err := insertComposeService(string(compose), minioService, "minio-data")
			if err != nil {
				return err
			}
			if err := os.WriteFile(composePath, []byte(updated), 0o644); err != nil { //nolint:gosec // compose.yaml is committed
				return err
			}
		}
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Set up"
	if *dryRun {
		verb = "Would set up"
	}
	fmt.Fprintf(stdout, "%s file storage with %s\n  Files:     %s\n  Variables: %s\n", verb, in.driver, strings.Join(files, ", "), strings.Join(envVars, ", "))
	switch in.driver {
	case "local":
		fmt.Fprintln(stdout, "\nFiles live under STORAGE_LOCAL_DIR (.orb/storage). Nothing else to do.")
	case "minio":
		fmt.Fprintln(stdout, "\nNext: orb dev starts MinIO from compose.yaml. Create the bucket once in its console (http://127.0.0.1:9001, minioadmin / minioadmin) or with `mc mb`.")
	default:
		fmt.Fprintln(stdout, "\nNext: put STORAGE_SECRET_KEY (and STORAGE_ACCESS_KEY, STORAGE_BUCKET if not given) in .env, then restart the app. See docs/guides/storage.md.")
	}
	return nil
}

// storageBlock renders the storage block of .env.example for a driver.
func storageBlock(in storageInput) string {
	var b strings.Builder
	b.WriteString("# orb:begin storage\n# File storage (ADR-0075): local keeps files under STORAGE_LOCAL_DIR on this\n# machine (development only); s3, spaces, r2 and minio are S3-compatible\n# services, set up with `orb add storage`. Production refuses local.\n")
	fmt.Fprintf(&b, "STORAGE_DRIVER=%s\nSTORAGE_LOCAL_DIR=.orb/storage\n", valueOr(in.driver, ""))
	b.WriteString("# For s3, spaces, r2 and minio: the endpoint (s3 and spaces derive it from\n# the region), region, bucket and keys; STORAGE_PUBLIC_URL when the bucket\n# is public; STORAGE_PATH_STYLE=true for MinIO (the default there).\n")
	fmt.Fprintf(&b, "STORAGE_ENDPOINT=%s\nSTORAGE_REGION=%s\nSTORAGE_BUCKET=%s\nSTORAGE_ACCESS_KEY=%s\nSTORAGE_SECRET_KEY=\nSTORAGE_PUBLIC_URL=%s\nSTORAGE_PATH_STYLE=\n", in.endpoint, in.region, in.bucket, exampleAccessKey(in), in.publicURL)
	if in.driver == "minio" {
		b.WriteString("# MinIO's host ports in compose.yaml.\nMINIO_PORT=9000\nMINIO_CONSOLE_PORT=9001\n")
	}
	b.WriteString("# Signs local storage's download and upload links; random at each start\n# when empty.\nSTORAGE_SIGNING_KEY=\n# orb:end storage\n")
	return b.String()
}

func exampleAccessKey(in storageInput) string {
	if in.driver == "minio" {
		return "minioadmin"
	}
	return ""
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// insertComposeService adds a service before the top-level volumes key,
// and the named volume to it.
func insertComposeService(compose, service, volume string) (string, error) {
	i := strings.Index(compose, "\nvolumes:")
	if i < 0 {
		return "", errors.New("compose.yaml has no top-level volumes key to insert the service before")
	}
	out := compose[:i] + service + compose[i:]
	if !strings.Contains(out, "\n  "+volume+":") {
		out = strings.TrimRight(out, "\n") + "\n  " + volume + ":\n"
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
