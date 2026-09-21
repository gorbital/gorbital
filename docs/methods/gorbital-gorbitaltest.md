# gorbital/gorbitaltest

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/gorbitaltest"
```

Package gorbitaltest tests a gorbital app through its real middleware stack, on its own PostgreSQL database per test (ADR-0028):

```go
func TestCreateBook(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(books.Module()), gorbital.WithMigrations(migrations.FS))

	res := app.As(gorbitaltest.User("usr_1", usecase.PermWrite)).Post("/v1/books", map[string]any{"title": "Dune"})
	res.AssertStatus(t, http.StatusCreated)

	app.Client().Get("/v1/books").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
}
```

[New](#New) creates a database on the server GORBITAL\_TEST\_DATABASE\_URL names, migrates it with gorbital.Migrate and builds the app with gorbital.New. Tests are skipped when the variable is unset, and fail instead with GORBITAL\_REQUIRE\_DB=1, as in CI.

Requests carry a principal set with [App.As](#App.As), as the authenticator would set it, so tests don't sign in. Email modules send and jobs they enqueue are stored as jobs, which [App.Mail](#App.Mail) and [App.Jobs](#App.Jobs) read back: workers don't run in tests, so nothing is delivered.

Don't build apps in parallel tests of one package (t.Parallel): Huma keeps its error constructor in a package-level variable, which each app sets to its own error mappings (openapi.InstallErrors), so two apps built at once race.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Constants: [`SignUpPassword`](#SignUpPassword)
- Functions: [`APIKey`](#APIKey), [`User`](#User)
- Types:
  - [`App`](#App): [`New`](#New), [`NewWithEnv`](#NewWithEnv), [`App.App`](#App.App), [`App.As`](#App.As), [`App.Client`](#App.Client), [`App.Config`](#App.Config), [`App.Jobs`](#App.Jobs), [`App.Mail`](#App.Mail), [`App.SignUp`](#App.SignUp)
  - [`Client`](#Client): [`Client.Delete`](#Client.Delete), [`Client.Do`](#Client.Do), [`Client.Get`](#Client.Get), [`Client.Patch`](#Client.Patch), [`Client.Post`](#Client.Post), [`Client.Put`](#Client.Put), [`Client.WithHeader`](#Client.WithHeader)
  - [`Job`](#Job)
  - [`Response`](#Response): [`Response.AssertProblem`](#Response.AssertProblem), [`Response.AssertStatus`](#Response.AssertStatus), [`Response.JSON`](#Response.JSON)

## Constants

<a id="SignUpPassword"></a>

```go
const SignUpPassword = "correct horse battery staple"
```

SignUpPassword is the password of the accounts [App.SignUp](#App.SignUp) creates.

*Since `v0.3.0 (unreleased)`*

## Functions

<a id="APIKey"></a>

### func APIKey

```go
func APIKey(userID string, scopes ...string) auth.Principal
```

APIKey returns the principal of a request authenticated with an API key of the user userID, scoped to scopes: the actor holds exactly those permissions, and guards that need a session, such as guard.RecentReauth, refuse it.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
p := gorbitaltest.APIKey("usr_1", "books.book.read")
fmt.Println(p.UserID, p.Scopes, p.SessionID == "", p.APIKeyID != "")
```

Output:

```text
usr_1 [books.book.read] true true
```

<a id="User"></a>

### func User

```go
func User(id string, permissions ...string) auth.Principal
```

User returns the principal of a signed-in user with a session holding permissions. The session signed in and verified a second factor just now, so guard.RecentReauth allows it.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
p := gorbitaltest.User("usr_1", "books.book.write")
fmt.Println(p.UserID, p.Permissions, p.SessionID != "", p.APIKeyID == "")
```

Output:

```text
usr_1 [books.book.write] true true
```

## Types

<a id="App"></a>

### type App

```go
type App struct {
	// contains filtered or unexported fields
}
```

An App is a gorbital app built for one test.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
})
```

<a id="New"></a>

#### func New

```go
func New(t testing.TB, opts ...gorbital.Option) *App
```

New builds an app with opts on a new, migrated database for the test, and closes it when the test ends. The configuration is development's defaults, with logs at warning level and above written to the test's output, and the log archive and local file storage in temporary directories. opts are applied after gorbitaltest's own, so a gorbital.WithLogger or gorbital.WithAuth of the test's wins.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.Client().Get("/v1/catalog/bok_1").AssertStatus(t, http.StatusOK)
})
```

<a id="NewWithEnv"></a>

#### func NewWithEnv

```go
func NewWithEnv(t testing.TB, env map[string]string, opts ...gorbital.Option) *App
```

NewWithEnv is [New](#New) with environment variables on top of development's defaults, as an app's .env sets them, such as AUTH\_ENCRYPTION\_KEYS for an app whose sign-in enrols authenticator apps. The process environment is never read. DATABASE\_URL is always the test's database.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	// The app's configuration, as .env would set it.
	app := gorbitaltest.NewWithEnv(t, map[string]string{"APP_MAX_BODY_BYTES": "64"}, gorbital.WithModules(booksModule()))
	long := strings.Repeat("a", 100)
	app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": long}).AssertProblem(t, http.StatusRequestEntityTooLarge, "request_too_large")
	if app.Config().MaxBodyBytes != 64 {
		t.Errorf("MaxBodyBytes = %d", app.Config().MaxBodyBytes)
	}
})
```

<a id="App.App"></a>

#### func (*App) App

```go
func (a *App) App() *gorbital.App
```

App returns the built app, for its handler and its dependencies, such as Deps().DB to prepare rows.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	// Prepare rows through the app's own pool.
	if _, err := app.App().Deps().DB.Exec(context.Background(), `SELECT 1`); err != nil {
		t.Fatal(err)
	}
})
```

<a id="App.As"></a>

#### func (*App) As

```go
func (a *App) As(p auth.Principal) *Client
```

As returns a client whose requests carry p, as the authenticator sets it for a signed-in caller: see [User](#User) and [APIKey](#APIKey).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusForbidden, "forbidden")
})
```

<a id="App.Client"></a>

#### func (*App) Client

```go
func (a *App) Client() *Client
```

Client returns a client whose requests carry no principal, as a caller that isn't signed in.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	// Deny by default: a route without guard.Public refuses anonymous callers.
	app.Client().Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
})
```

<a id="App.Config"></a>

#### func (*App) Config

```go
func (a *App) Config() gorbital.Config
```

Config returns the configuration the app was built with, such as for running a command of the authenticator (gorbital.Command) against the test's database.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	// Commands such as authhttp's grant-role open the test's database
	// from the configuration.
	if app.Config().DatabaseURL.IsZero() {
		t.Error("no database URL")
	}
})
```

<a id="App.Jobs"></a>

#### func (*App) Jobs

```go
func (a *App) Jobs(t testing.TB, kind string) []Job
```

Jobs returns the jobs the app enqueued of kind, oldest first, or every job when kind is empty.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	if n := len(app.Jobs(t, "gorbital.mail.send")); n != 1 {
		t.Errorf("queued emails = %d, want 1", n)
	}
})
```

<a id="App.Mail"></a>

#### func (*App) Mail

```go
func (a *App) Mail(t testing.TB) []mail.Message
```

Mail returns the email the app queued, oldest first, with the sender the mail.\* runtime settings filled in.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	sent := app.Mail(t)
	if len(sent) != 1 || sent[0].Subject != "Added Dune" || sent[0].To[0].Email != "reader@example.com" {
		t.Errorf("mail = %+v", sent)
	}
})
```

<a id="App.SignUp"></a>

#### func (*App) SignUp

```go
func (a *App) SignUp(t testing.TB, email string) (*Client, string)
```

SignUp creates an account for email through sign-in's own endpoints, in an app with gorbital.dev/gorbital/authhttp passed to gorbital.WithAuth: it registers with [SignUpPassword](#SignUpPassword), verifies the address with the code from the queued email, and signs in with a bearer token. It returns a client whose requests carry the token, and the account's user ID.

The account is real: hooks run (such as the personal workspace of gorbital.dev/gorbital/orgshttp), and its requests go through the authenticator, API key scopes and second factors included, so tests of organisations and other features that store the user's ID use it rather than [User](#User).

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	// Real accounts, for features that store who the user is, such as
	// organisations: sign-in and orgshttp are the app's.
	auth := authhttp.New()
	app := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": authlib.NewKeyringKey("test")},
		gorbital.WithAuth(auth), gorbital.WithModules(orgshttp.Module(auth)))
	ada, adaID := app.SignUp(t, "ada@example.com")
	res := ada.Get("/v1/orgs")
	res.AssertStatus(t, http.StatusOK)
	var orgs struct {
		Items []struct {
			Personal bool `json:"personal"`
		} `json:"items"`
	}
	res.JSON(t, &orgs)
	if adaID == "" || len(orgs.Items) != 1 || !orgs.Items[0].Personal {
		t.Errorf("ada %q has organisations %+v, want her personal workspace", adaID, orgs.Items)
	}
})
```

<a id="Client"></a>

### type Client

```go
type Client struct {
	// contains filtered or unexported fields
}
```

A Client sends requests to an app's handler, through its whole middleware stack.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	writer := app.As(gorbitaltest.User("usr_1", "books.book.write"))
	writer.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
})
```

<a id="Client.Delete"></a>

#### func (*Client) Delete

```go
func (c *Client) Delete(path string) *Response
```

Delete sends a DELETE request for path.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1")).Delete("/v1/books/bok_1").AssertProblem(t, http.StatusNotFound, "not_found")
})
```

<a id="Client.Do"></a>

#### func (*Client) Do

```go
func (c *Client) Do(req *http.Request) *Response
```

Do sends req as it is, with the client's principal and headers added.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	req := httptest.NewRequest(http.MethodPost, "/v1/books", strings.NewReader(`{"title": "Dune"`))
	req.Header.Set("Content-Type", "application/json")
	app.As(gorbitaltest.User("usr_1", "books.book.write")).Do(req).AssertProblem(t, http.StatusBadRequest, "bad_request")
})
```

<a id="Client.Get"></a>

#### func (*Client) Get

```go
func (c *Client) Get(path string) *Response
```

Get sends a GET request for path, which may have a query string.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.Client().Get("/v1/catalog/bok_1?fields=title").AssertStatus(t, http.StatusOK)
})
```

<a id="Client.Patch"></a>

#### func (*Client) Patch

```go
func (c *Client) Patch(path string, body any) *Response
```

Patch sends a PATCH request with body encoded as JSON.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1")).Patch("/v1/books/bok_1", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusNotFound, "not_found")
})
```

<a id="Client.Post"></a>

#### func (*Client) Post

```go
func (c *Client) Post(path string, body any) *Response
```

Post sends a POST request with body encoded as JSON; a nil body sends none.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	res := app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": ""})
	res.AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
})
```

<a id="Client.Put"></a>

#### func (*Client) Put

```go
func (c *Client) Put(path string, body any) *Response
```

Put sends a PUT request with body encoded as JSON.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.As(gorbitaltest.User("usr_1")).Put("/v1/books/bok_1", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusNotFound, "not_found")
})
```

<a id="Client.WithHeader"></a>

#### func (*Client) WithHeader

```go
func (c *Client) WithHeader(name, value string) *Client
```

WithHeader returns a copy of c that sends the header on every request, such as Idempotency-Key.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	writer := app.As(gorbitaltest.User("usr_1", "books.book.write")).WithHeader("Idempotency-Key", "7f1e9c4a")
	writer.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	retry := writer.Post("/v1/books", map[string]string{"title": "Dune"})
	if retry.Header.Get("Idempotent-Replayed") != "true" {
		t.Error("the retry wasn't replayed")
	}
})
```

<a id="Job"></a>
<a id="Job.Kind"></a>
<a id="Job.Args"></a>

### type Job

```go
type Job struct {
	// Kind is the job's name, such as "gorbital.mail.send".
	Kind string
	// Args are the job's arguments as stored, in JSON.
	Args json.RawMessage
}
```

A Job is a job the app enqueued.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	for _, j := range app.Jobs(t, "") {
		var args map[string]any
		if err := json.Unmarshal(j.Args, &args); err != nil {
			t.Fatal(err)
		}
		t.Log(j.Kind, args)
	}
})
```

<a id="Response"></a>
<a id="Response.Status"></a>
<a id="Response.Header"></a>
<a id="Response.Body"></a>

### type Response

```go
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	// contains filtered or unexported fields
}
```

A Response is what the app answered.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	res := app.Client().Get("/v1/catalog/bok_1")
	t.Log(res.Status, res.Header.Get("Content-Type"), string(res.Body))
})
```

<a id="Response.AssertProblem"></a>

#### func (*Response) AssertProblem

```go
func (r *Response) AssertProblem(t testing.TB, status int, code string)
```

AssertProblem fails the test unless the response is a problem+json error with status and code, such as 404 and "book\_not\_found".

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.Client().Get("/v1/shelves").AssertProblem(t, http.StatusNotFound, "not_found")
})
```

<a id="Response.AssertStatus"></a>

#### func (*Response) AssertStatus

```go
func (r *Response) AssertStatus(t testing.TB, want int)
```

AssertStatus fails the test when the response's status isn't want, showing the body.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	app.Client().Get("/readyz").AssertStatus(t, http.StatusOK)
})
```

<a id="Response.JSON"></a>

#### func (*Response) JSON

```go
func (r *Response) JSON(t testing.TB, v any)
```

JSON decodes the body into v, failing the test when it isn't JSON of that shape.

*Since `v0.3.0 (unreleased)`*

**Example**

```go
test(func(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
	var book struct{ Title string }
	app.Client().Get("/v1/catalog/bok_1").JSON(t, &book)
	if book.Title != "Dune" {
		t.Errorf("title = %q", book.Title)
	}
})
```
