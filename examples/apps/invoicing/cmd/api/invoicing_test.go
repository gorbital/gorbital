package main_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/modules/postgres"

	"example.com/invoicing/db/migrations"
	"example.com/invoicing/internal/modules"
	authhttp "example.com/invoicing/internal/modules/auth"
	orgshttp "example.com/invoicing/internal/modules/orgs"
)

// newApp builds the app with main.go's options, on a new database for the
// test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithName("invoicing"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// createCompany creates an organisation named name, owned by client, and
// returns its ID.
func createCompany(t *testing.T, client *gorbitaltest.Client, name string) string {
	t.Helper()
	res := client.Post("/v1/orgs", map[string]string{"name": name})
	res.AssertStatus(t, http.StatusCreated)
	var company struct {
		ID string `json:"id"`
	}
	res.JSON(t, &company)
	return company.ID
}

// createInvoice creates an invoice numbered number in the company and
// returns its ID.
func createInvoice(t *testing.T, client *gorbitaltest.Client, companyID, number, customer string) string {
	t.Helper()
	res := client.Post("/v1/orgs/"+companyID+"/invoices", map[string]string{"number": number, "customer": customer})
	res.AssertStatus(t, http.StatusCreated)
	var invoice struct {
		ID string `json:"id"`
	}
	res.JSON(t, &invoice)
	return invoice.ID
}

// invitationToken returns the token of the last invitation emailed to
// email, from the link's #token= fragment.
func invitationToken(t *testing.T, app *gorbitaltest.App, email string) string {
	t.Helper()
	token := ""
	for _, m := range app.Mail(t) {
		if len(m.To) == 0 || m.To[0].Email != email {
			continue
		}
		if _, after, ok := strings.Cut(m.Text, "#token="); ok {
			token, _, _ = strings.Cut(after, "\n")
			token = strings.TrimSpace(token)
		}
	}
	if token == "" {
		t.Fatalf("no invitation was emailed to %s", email)
	}
	return token
}

// docs:start companies

// TestCompanies: each company is an organisation. Ada creates Acme and
// invites Bob, who joins through the emailed link and bills a customer.
// Carol runs Globex: for her Acme doesn't exist, and Acme's invoice isn't
// found under Globex either.
func TestCompanies(t *testing.T) {
	app := newApp(t)
	ada, _ := app.SignUp(t, "ada@acme.example")
	bob, _ := app.SignUp(t, "bob@acme.example")
	carol, _ := app.SignUp(t, "carol@globex.example")

	acme := createCompany(t, ada, "Acme Ltd")
	globex := createCompany(t, carol, "Globex Corporation")
	acmeInvoices := "/v1/orgs/" + acme + "/invoices"

	// Bob joins Acme through the link in his invitation email.
	ada.Post("/v1/orgs/"+acme+"/invitations", map[string]string{"email": "bob@acme.example", "role": "member"}).
		AssertStatus(t, http.StatusCreated)
	bob.Get(acmeInvoices).AssertProblem(t, http.StatusNotFound, "org_not_found") // not a member yet
	bob.Post("/v1/invitations/accept", map[string]string{"token": invitationToken(t, app, "bob@acme.example")}).
		AssertStatus(t, http.StatusOK)

	invoice := createInvoice(t, bob, acme, "ACME-0001", "Wayne Enterprises")
	ada.Post(acmeInvoices, map[string]string{"number": "acme-0001", "customer": "Stark Industries"}).
		AssertProblem(t, http.StatusConflict, "invoice_number_taken")
	ada.Get(acmeInvoices+"/"+invoice).AssertStatus(t, http.StatusOK)

	// Numbers are unique per company: Globex has its own ACME-0001.
	createInvoice(t, carol, globex, "ACME-0001", "Initech")

	// Carol isn't a member of Acme: every route answers as for an unknown
	// company, and Acme's invoice isn't one of Globex's.
	carol.Get(acmeInvoices).AssertProblem(t, http.StatusNotFound, "org_not_found")
	carol.Get(acmeInvoices+"/"+invoice).AssertProblem(t, http.StatusNotFound, "org_not_found")
	carol.Get("/v1/orgs/"+globex+"/invoices/"+invoice).AssertProblem(t, http.StatusNotFound, "invoice_not_found")
	bob.Get("/v1/orgs/"+globex+"/invoices").AssertProblem(t, http.StatusNotFound, "org_not_found")
}

// docs:end companies

// docs:start row-level-security

// TestRowLevelSecurity: connected as a role without superuser or BYPASSRLS,
// as production's DATABASE_URL must be, the database itself keeps each
// company's invoices apart, whatever the SQL says.
func TestRowLevelSecurity(t *testing.T) {
	app := newApp(t)
	ada, _ := app.SignUp(t, "ada@acme.example")
	carol, _ := app.SignUp(t, "carol@globex.example")
	acme, globex := createCompany(t, ada, "Acme Ltd"), createCompany(t, carol, "Globex Corporation")
	createInvoice(t, ada, acme, "ACME-0001", "Wayne Enterprises")
	createInvoice(t, ada, acme, "ACME-0002", "Stark Industries")
	createInvoice(t, carol, globex, "GLX-0001", "Initech")

	ctx := context.Background()
	db, err := postgres.Open(ctx, config.NewSecret(asAppRole(t, app.Config().DatabaseURL.Reveal())))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// No org_id filter: the policy adds it. A context acting in Acme, as
	// guard.OrgMember gives the handler, sees only Acme's invoices.
	if got := invoiceNumbers(t, postgres.WithOrg(ctx, acme), db); !slices.Equal(got, []string{"ACME-0001", "ACME-0002"}) {
		t.Errorf("invoices seen acting in Acme = %v, want Acme's two", got)
	}
	// A context without an organisation, such as a sign-in, sees none.
	if got := invoiceNumbers(t, ctx, db); len(got) != 0 {
		t.Errorf("invoices seen without an organisation = %v, want none", got)
	}

	// Writing a row of Globex while acting in Acme is refused.
	_, err = db.Exec(postgres.WithOrg(ctx, acme), `
		INSERT INTO invoices (id, org_id, created_by, number, customer, created_at, updated_at)
		VALUES ('inv_forged', $1, 'usr_forged', 'GLX-9999', 'Nobody', now(), now())`, globex)
	if pgErr := (*pgconn.PgError)(nil); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("insert into Globex acting in Acme = %v, want 42501 (insufficient_privilege)", err)
	}

	// The test database's own user is a superuser: no policy applies to it,
	// which is why the app's role must be neither superuser nor BYPASSRLS.
	superuser, err := postgres.Open(ctx, app.Config().DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer superuser.Close()
	if got := invoiceNumbers(t, postgres.WithOrg(ctx, acme), superuser); len(got) != 3 {
		t.Errorf("invoices a superuser sees acting in Acme = %v, want all 3", got)
	}
}

// docs:end row-level-security

// invoiceNumbers runs a query without an org_id filter and returns the
// numbers it sees, sorted.
func invoiceNumbers(t *testing.T, ctx context.Context, db postgres.DBTX) []string {
	t.Helper()
	rows, err := db.Query(ctx, "SELECT number FROM invoices ORDER BY number")
	if err != nil {
		t.Fatal(err)
	}
	numbers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return numbers
}

// appRole is a database role like production's: no superuser, no
// BYPASSRLS.
const appRole = "invoicing_app_test"

// asAppRole returns dbURL connecting as appRole, which it creates on the
// test server once and grants the test database's tables.
func asAppRole(t *testing.T, dbURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	role := pgx.Identifier{appRole}.Sanitize()
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Test binaries run in parallel; the lock lets one create the role.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('invoicing_app_test.role', 0))"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", appRole).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOBYPASSRLS"); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			GRANT USAGE ON SCHEMA public TO `+role+`;
			GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO `+role)
		return err
	})
	if err != nil {
		t.Fatalf("set up database role %s: %v", appRole, err)
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", "-c role="+appRole)
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20") // pgx doesn't read + as a space
	return u.String()
}
