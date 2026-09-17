package gorbital_test

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/mail"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/storage"
)

// migrationFiles stands in for an app's db/migrations package.
var migrationFiles embed.FS

// modulesAll stands in for the generated internal/modules.All.
func modulesAll() []gorbital.Module { return nil }

// developmentEnv is a configuration source for the examples.
func developmentEnv(vars map[string]string) config.Source {
	return config.Source{Getenv: func(k string) string { return vars[k] }}
}

func ExampleMain() {
	// cmd/api/main.go of an app: go run ./cmd/api serves it, go run ./cmd/api
	// migrate migrates its database.
	main := func() {
		gorbital.Main(
			gorbital.WithModules(modulesAll()...),
			gorbital.WithMigrations(migrationFiles),
		)
	}
	_ = main
}

func ExampleLoadConfig() {
	cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
		"APP_ENV":          "production",
		"APP_ADDR":         "api",
		"APP_CORS_ORIGINS": "http://app.example.com",
		"STORAGE_DRIVER":   "local",
	}))
	fmt.Println(cfg.Env == "") // the zero Config on error
	for line := range strings.Lines(err.Error()) {
		fmt.Print(line)
	}
	// Output:
	// true
	// invalid configuration:
	// APP_ADDR "api" is not host:port: address api: missing port in address
	// APP_CORS_ORIGINS: "http://app.example.com" must use https in production
	// STORAGE_DRIVER=local is for development; production needs s3, spaces, r2 or minio with STORAGE_ENDPOINT, STORAGE_BUCKET, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY
}

func ExampleConfig() {
	cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "development", "APP_JOB_WORKERS": "4"}))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Addr, cfg.JobWorkers, cfg.MailDelivery, cfg.DocsEnabled)
	// Output: 127.0.0.1:8080 4 devmail true
}

func ExampleConfig_Production() {
	cfg, _ := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "development"}))
	fmt.Println(cfg.Production())
	// Output: false
}

func ExampleMailConfig() {
	// An email provider built from its secrets, for MAIL_DELIVERY=provider.
	provider := gorbital.WithMailerFunc(func(cfg gorbital.Config) (mail.Sender, error) {
		if cfg.Mail.ResendAPIKey.IsZero() {
			return nil, errors.New("RESEND_API_KEY is required to send email with Resend")
		}
		return newResendSender(cfg.Mail.ResendAPIKey), nil
	})
	_ = provider
}

// newResendSender stands in for resend.New.
func newResendSender(config.Secret) mail.Sender {
	return mail.SenderFunc(func(context.Context, mail.Message) error { return nil })
}

func ExampleStorageConfig() {
	cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
		"APP_ENV": "development", "STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1",
		"STORAGE_BUCKET": "files", "STORAGE_ACCESS_KEY": "AKIA", "STORAGE_SECRET_KEY": "secret",
	}))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Storage.Driver, cfg.Storage.Endpoint, cfg.Storage.Bucket, cfg.Storage.SecretKey)
	// Output: s3 s3.eu-west-1.amazonaws.com files [redacted]
}

func ExampleAuthConfig() {
	cfg, err := gorbital.LoadConfig(developmentEnv(map[string]string{
		"APP_ENV": "development", "GITHUB_CLIENT_ID": "Iv1.abc", "GITHUB_CLIENT_SECRET": "secret",
	}))
	if err != nil {
		panic(err)
	}
	fmt.Println(cfg.Auth.PublicURL, cfg.Auth.DefaultReturnTo, cfg.Auth.WebAuthnRPID)
	// Output: http://localhost:8080 http://localhost:8080/docs localhost
}

func ExampleDevConsoleConfig() {
	_, err := gorbital.LoadConfig(developmentEnv(map[string]string{"APP_ENV": "production", "DEV_CONSOLE_TOKEN": strings.Repeat("x", 40)}))
	fmt.Println(strings.Contains(err.Error(), "DEV_CONSOLE_TOKEN is for local development only"))
	// Output: true
}

func ExampleNew() {
	// What Main does for the serve command, for an app that builds its own
	// program or embeds the handler in another server.
	ctx := context.Background()
	cfg, err := gorbital.LoadConfig(config.OS)
	if err != nil {
		log.Fatal(err)
	}
	opts := []gorbital.Option{gorbital.WithModules(modulesAll()...), gorbital.WithMigrations(migrationFiles)}
	if err := gorbital.Migrate(ctx, cfg, os.Stdout, opts...); err != nil {
		log.Fatal(err)
	}
	app, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func ExampleApp() {
	serve := func(ctx context.Context, cfg gorbital.Config) error {
		app, err := gorbital.New(ctx, cfg, gorbital.WithModules(modulesAll()...))
		if err != nil {
			return err
		}
		return app.Run(ctx) // until SIGINT or SIGTERM
	}
	_ = serve
}

func ExampleApp_Run() {
	run := func(ctx context.Context, app *gorbital.App) error {
		// Run stops when ctx is done, as well as on a signal.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		return app.Run(ctx)
	}
	_ = run
}

func ExampleApp_Handler() {
	mount := func(app *gorbital.App) http.Handler {
		// The app under /api/ of another server.
		mux := http.NewServeMux()
		mux.Handle("/api/", http.StripPrefix("/api", app.Handler()))
		return mux
	}
	_ = mount
}

func ExampleApp_Deps() {
	seed := func(ctx context.Context, app *gorbital.App) error {
		_, err := app.Deps().DB.Exec(ctx, `INSERT INTO books (id, title) VALUES ('bok_1', 'Dune') ON CONFLICT DO NOTHING`)
		return err
	}
	_ = seed
}

func ExampleApp_Close() {
	check := func(ctx context.Context, cfg gorbital.Config) (err error) {
		app, err := gorbital.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, app.Close(ctx)) }()
		// Use the app without running it.
		return nil
	}
	_ = check
}

func ExampleMigrate() {
	migrate := func(ctx context.Context, cfg gorbital.Config) error {
		// Prints "applied migration <version>" for each migration applied.
		return gorbital.Migrate(ctx, cfg, os.Stdout, gorbital.WithMigrations(migrationFiles))
	}
	_ = migrate
}

func ExampleMigration() {
	// A built-in module serving its embedded migration under the version it
	// has in apps' histories.
	reviews := gorbital.Module{
		Name: "reviews",
		Migrations: []gorbital.Migration{
			{Version: 20270301000001, Name: "reviews", FS: migrationFiles, File: "00001_reviews.sql"},
		},
	}
	fmt.Println(reviews.Migrations[0].Version, reviews.Migrations[0].Name)
	// Output: 20270301000001 reviews
}

func ExampleOption() {
	opts := []gorbital.Option{
		gorbital.WithName("shelfie"),
		gorbital.WithModules(modulesAll()...),
	}
	_ = opts
}

func ExampleWithName() {
	gorbital.Main(gorbital.WithName("shelfie"), gorbital.WithModules(modulesAll()...))
}

func ExampleWithModules() {
	gorbital.Main(gorbital.WithModules(modulesAll()...))
}

// headerAuth is an Authenticator trusting a gateway's header, for the
// examples.
type headerAuth struct{}

func (headerAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get("X-Authenticated-User"); id != "" {
				r = r.WithContext(actor.With(r.Context(), actor.Actor{Kind: actor.KindUser, ID: id}))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ExampleAuthenticator() {
	var a gorbital.Authenticator = headerAuth{}
	gorbital.Main(gorbital.WithAuth(a), gorbital.WithModules(modulesAll()...))
}

func ExampleWithAuth() {
	gorbital.Main(gorbital.WithAuth(headerAuth{}), gorbital.WithModules(modulesAll()...))
}

func ExampleWithStorage() {
	var bucket storage.Store // such as a store from gorbital.dev/modules/storage/s3
	gorbital.Main(gorbital.WithStorage(bucket))
}

func ExampleWithStorageFunc() {
	gorbital.Main(gorbital.WithStorageFunc(func(cfg gorbital.Config) (storage.Store, error) {
		// With gorbital.dev/modules/storage/s3:
		//	return s3.New(s3.Config{Driver: cfg.Storage.Driver, Endpoint: cfg.Storage.Endpoint, ...})
		return nil, fmt.Errorf("STORAGE_DRIVER=%s isn't set up in this app", cfg.Storage.Driver)
	}))
}

func ExampleWithMailer() {
	logged := mail.SenderFunc(func(ctx context.Context, m mail.Message) error {
		slog.InfoContext(ctx, "email", "subject", m.Subject)
		return nil
	})
	gorbital.Main(gorbital.WithMailer(logged))
}

func ExampleWithMailerFunc() {
	gorbital.Main(gorbital.WithMailerFunc(func(cfg gorbital.Config) (mail.Sender, error) {
		return newResendSender(cfg.Mail.ResendAPIKey), nil
	}))
}

// requireClientVersion refuses clients older than min, for the examples.
func requireClientVersion(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get("X-Client-Version"); v != "" && v < min {
				http.Error(w, "upgrade the app", http.StatusUpgradeRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ExampleWithMiddleware() {
	gorbital.Main(gorbital.WithMiddleware(requireClientVersion("2.4.0")))
}

func ExampleWithMiddlewareFunc() {
	gorbital.Main(gorbital.WithMiddlewareFunc(func(d gorbital.Deps) func(http.Handler) http.Handler {
		logger := d.Logger
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if a, ok := actor.From(r.Context()); ok {
					logger.DebugContext(r.Context(), "request", "actor", a.ID)
				}
				next.ServeHTTP(w, r)
			})
		}
	}))
}

func ExampleWithStack() {
	gorbital.Main(gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
		// Refuse old clients before anything is logged or authenticated.
		return append([]func(http.Handler) http.Handler{s.Recover, requireClientVersion("2.4.0")}, s.Default()[1:]...)
	}))
}

func ExampleWithLogger() {
	gorbital.Main(gorbital.WithLogger(slog.New(slog.NewJSONHandler(os.Stderr, nil))))
}

func ExampleWithMigrations() {
	// db/migrations/migrations.go:
	//
	//	//go:embed *.sql
	//	var FS embed.FS
	gorbital.Main(gorbital.WithMigrations(migrationFiles))
}

func ExampleStack() {
	name := func(label string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Print(label, " ")
				next.ServeHTTP(w, r)
			})
		}
	}
	s := gorbital.Stack{Recover: name("recover"), RequestID: name("request-id"), Auth: name("auth")}
	var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { fmt.Println("handler") })
	for _, mw := range []func(http.Handler) http.Handler{s.Auth, s.RequestID, s.Recover} {
		h = mw(h)
	}
	h.ServeHTTP(nil, nil)
	// Output: recover request-id auth handler
}

func ExampleStack_Default() {
	var s gorbital.Stack
	fmt.Println(len(s.Default()))
	// Output: 14
}

func ExampleCommand() {
	// A command an authenticator contributes from its Commands method.
	grantRole := gorbital.Command{
		Name:  "grant-role",
		Usage: "grant-role <email> <role>      give an account a platform role",
		Run: func(ctx context.Context, cfg gorbital.Config, args []string, stdout io.Writer) error {
			if len(args) != 2 {
				return fmt.Errorf("%w: grant-role <email> <role>", gorbital.ErrUsage)
			}
			fmt.Fprintf(stdout, "%s is now %s\n", args[0], args[1])
			return nil
		},
	}
	err := grantRole.Run(context.Background(), gorbital.Config{}, []string{"ada@example.com"}, os.Stdout)
	fmt.Println(errors.Is(err, gorbital.ErrUsage))
	// Output: true
}

func ExampleModule_jobs() {
	// Jobs runs before the job client exists: keep d and use it when a job
	// runs.
	digest := gorbital.Module{
		Name: "digests",
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			// jobs.Define(defs, jobs.Definition[DigestArgs]{Name: "digests_send", Worker: &digestWorker{mailer: d.Mailer}, ...})
			_ = d.Mailer
		},
	}
	_ = digest
}
