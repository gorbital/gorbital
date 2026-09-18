package notifications_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/orgshttp"

	"example.com/plateful/db/migrations"
	"example.com/plateful/internal/modules/notifications"
	"example.com/plateful/internal/modules/notifications/domain"
	"example.com/plateful/internal/modules/notifications/repository"
	"example.com/plateful/internal/modules/notifications/usecase"
)

// These tests drive the notifications routes through the app's real
// middleware stack, on a new database per test (gorbitaltest), and drive the
// two workers by hand: workers don't run in tests, so the way to test one is
// to call its Work with a job you build yourself.
//
// Two apps appear below. The strict one, notifications.Module() with no
// option, is what production is: it refuses to deliver anywhere private. The
// permissive one, notifications.Module(notifications.AllowPrivateTargets(true)),
// is what development is, and it is the only way a test can point an
// endpoint at an httptest server on 127.0.0.1.

// newApp builds the app with sign-in, organisations and the notifications
// module built with opts.
func newApp(t *testing.T, opts ...notifications.Option) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), notifications.Module(opts...)),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets, where it
// is the owner.
func signUp(t *testing.T, app *gorbitaltest.App, email string) (*gorbitaltest.Client, string, string) {
	t.Helper()
	client, userID := app.SignUp(t, email)
	var orgs struct {
		Items []struct {
			ID       string `json:"id"`
			Personal bool   `json:"personal"`
		} `json:"items"`
	}
	client.Get("/v1/orgs").JSON(t, &orgs)
	for _, org := range orgs.Items {
		if org.Personal {
			return client, userID, org.ID
		}
	}
	t.Fatalf("%s has no personal workspace", email)
	return nil, "", ""
}

// collection is the path of the organisation orgID's notification endpoints.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/notification-endpoints" }

// apiEndpoint is a notification endpoint as the API returns it. There is
// deliberately no URL field to decode into: the API has none.
type apiEndpoint struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Host           string `json:"host"`
	LastDeliveryAt string `json:"last_delivery_at"`
	LastStatus     int    `json:"last_status"`
	LastError      string `json:"last_error"`
	CreatedBy      string `json:"created_by"`
}

// register adds one endpoint and returns it.
func register(t *testing.T, client *gorbitaltest.Client, orgID, label, url string) apiEndpoint {
	t.Helper()
	res := client.Post(collection(orgID), map[string]any{"label": label, "url": url})
	res.AssertStatus(t, http.StatusCreated)
	var e apiEndpoint
	res.JSON(t, &e)
	return e
}

// list returns the organisation's endpoints and the raw response body, so a
// test can search the body for things that must not be in it.
func list(t *testing.T, client *gorbitaltest.Client, orgID string) ([]apiEndpoint, string) {
	t.Helper()
	res := client.Get(collection(orgID))
	res.AssertStatus(t, http.StatusOK)
	var page struct {
		Items []apiEndpoint `json:"items"`
	}
	res.JSON(t, &page)
	return page.Items, string(res.Body)
}

// docs:start notification-receiver

// receiver is an httptest server standing in for a restaurant's chat: it
// records what was posted to it and answers with whatever status the test
// set.
type receiver struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	bodies []string
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: http.StatusOK}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 64<<10))
		r.mu.Lock()
		r.bodies = append(r.bodies, string(body))
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(r.Close)
	return r
}

// docs:end notification-receiver

func (r *receiver) answer(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (r *receiver) posted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.bodies...)
}

// deliveryWorker builds the delivery worker the module builds, with a sender
// that allows private targets so it can reach the test's own server.
func deliveryWorker(app *gorbitaltest.App) *usecase.DeliveryWorker {
	return usecase.NewDeliveryWorker(
		repository.NewStore(app.App().Deps().DB),
		notifications.NewSender(domain.Policy{AllowPrivateTargets: true}),
		app.App().Deps().Audit,
		nil,
	)
}

// deliveryJob builds the job River would hand a worker. attempt and
// maxAttempts are what the worker's retry decisions are made from, so a test
// sets them to say "this is the first of five" or "this is the last one".
func deliveryJob(args usecase.DeliveryArgs, attempt, maxAttempts int) *river.Job[usecase.DeliveryArgs] {
	return &river.Job[usecase.DeliveryArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: usecase.DeliveryJob, Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   args,
	}
}

// auditCount is how many audit events of action the app recorded for orgID.
func auditCount(t *testing.T, app *gorbitaltest.App, action, orgID string) int {
	t.Helper()
	var n int
	err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = $1 AND coalesce(org_id, '') = $2`, action, orgID).Scan(&n)
	if err != nil {
		t.Fatalf("count %s audit events: %v", action, err)
	}
	return n
}

// TestEndpointIsRegisteredAndItsURLNeverComesBack: the whole point of the
// module's API surface. The URL goes in and is never seen again — not in the
// 201, not in the list, not in any part of either body.
func TestEndpointIsRegisteredAndItsURLNeverComesBack(t *testing.T) {
	app := newApp(t)
	client, userID, orgID := signUp(t, app, "ada@example.com")

	// The path is the credential: this string is what must never reappear.
	const secretPath = "/services/T00000/B00000/ZZTOPSECRETTOKEN"
	const webhook = "https://hooks.example.com" + secretPath

	res := client.Post(collection(orgID), map[string]any{"label": "Kitchen", "url": webhook})
	res.AssertStatus(t, http.StatusCreated)
	if body := string(res.Body); strings.Contains(body, secretPath) || strings.Contains(body, "ZZTOPSECRETTOKEN") {
		t.Fatalf("the 201 body contains the webhook URL: %s", body)
	}
	var created apiEndpoint
	res.JSON(t, &created)
	switch {
	case created.ID == "":
		t.Fatal("the 201 has no endpoint ID")
	case created.Label != "Kitchen":
		t.Errorf("label = %q, want Kitchen", created.Label)
	case created.Host != "hooks.example.com":
		t.Errorf("host = %q, want hooks.example.com", created.Host)
	case created.CreatedBy != userID:
		t.Errorf("created_by = %q, want %q", created.CreatedBy, userID)
	case created.LastStatus != 0 || created.LastDeliveryAt != "":
		t.Errorf("a new endpoint reports a delivery: %+v", created)
	}

	items, body := list(t, client, orgID)
	if len(items) != 1 || items[0].ID != created.ID || items[0].Host != "hooks.example.com" {
		t.Fatalf("list = %+v, want the one endpoint", items)
	}
	if strings.Contains(body, secretPath) || strings.Contains(body, "ZZTOPSECRETTOKEN") {
		t.Fatalf("the list body contains the webhook URL: %s", body)
	}

	// A second endpoint with the same label, ignoring case, is a conflict.
	client.Post(collection(orgID), map[string]any{"label": "kitchen", "url": "https://hooks.example.com/other"}).
		AssertProblem(t, http.StatusConflict, "endpoint_label_taken")

	// Removing it is how a leaked URL is revoked, so it has to work.
	client.Delete(collection(orgID)+"/"+created.ID).AssertStatus(t, http.StatusNoContent)
	client.Delete(collection(orgID)+"/"+created.ID).AssertProblem(t, http.StatusNotFound, "endpoint_not_found")
	if items, _ := list(t, client, orgID); len(items) != 0 {
		t.Fatalf("after removing: %+v, want no endpoints", items)
	}
}

// TestUnacceptableURLsAreRefused: with the strict policy — the one
// production runs — a URL that isn't https, one carrying a user name and
// password, and one naming a loopback address are all refused before
// anything is stored, and the answer never quotes the URL back.
func TestUnacceptableURLsAreRefused(t *testing.T) {
	app := newApp(t) // no AllowPrivateTargets: this is production's policy
	client, _, orgID := signUp(t, app, "ada@example.com")

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"plain http", "http://hooks.example.com/services/T0/B0/SECRET"},
		{"user information", "https://ada:hunter2@hooks.example.com/services/T0/B0/SECRET"},
		{"loopback address", "https://127.0.0.1:9200/services/T0/B0/SECRET"},
		{"fragment", "https://hooks.example.com/services/T0/B0/SECRET#frag"},
		{"no host", "https:///services/T0/B0/SECRET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := client.Post(collection(orgID), map[string]any{"label": tc.name, "url": tc.url})
			res.AssertProblem(t, http.StatusUnprocessableEntity, "invalid_endpoint_url")
			if strings.Contains(string(res.Body), "SECRET") {
				t.Errorf("the problem quotes the URL back: %s", res.Body)
			}
		})
	}
	if items, _ := list(t, client, orgID); len(items) != 0 {
		t.Fatalf("a refused URL was stored: %+v", items)
	}
}

// TestDeliverySucceeds: the delivery worker posts the Slack-style body,
// reports the status on the endpoint and records notifications.delivery.sent
// as the system, under the right organisation.
func TestDeliverySucceeds(t *testing.T) {
	app := newApp(t, notifications.AllowPrivateTargets(true))
	client, _, orgID := signUp(t, app, "ada@example.com")
	server := newReceiver(t)
	endpoint := register(t, client, orgID, "Kitchen", server.URL+"/hook")

	worker := deliveryWorker(app)
	args := usecase.DeliveryArgs{
		EndpointID: endpoint.ID, OrgID: orgID,
		Title: "New order", Lines: []string{"Order ord_7", "Total 24.50 GBP"},
	}
	if err := worker.Work(context.Background(), deliveryJob(args, 1, 5)); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}

	posted := server.posted()
	if len(posted) != 1 {
		t.Fatalf("the endpoint received %d requests, want 1", len(posted))
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(posted[0]), &body); err != nil {
		t.Fatalf("the body is not JSON: %s", posted[0])
	}
	if want := "New order\nOrder ord_7\nTotal 24.50 GBP"; body.Text != want {
		t.Errorf("text = %q, want %q", body.Text, want)
	}

	if n := auditCount(t, app, usecase.ActionSent, orgID); n != 1 {
		t.Errorf("%s events = %d, want 1", usecase.ActionSent, n)
	}
	var actorKind, actorID string
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT actor_kind, actor_id FROM audit_events WHERE action = $1`, usecase.ActionSent).
		Scan(&actorKind, &actorID); err != nil {
		t.Fatalf("read the audit event: %v", err)
	}
	if actorKind != "system" || actorID != usecase.DeliveryJob {
		t.Errorf("the delivery event was recorded as %s/%s, want system/%s", actorKind, actorID, usecase.DeliveryJob)
	}

	items, body2 := list(t, client, orgID)
	if len(items) != 1 || items[0].LastStatus != http.StatusOK || items[0].LastDeliveryAt == "" {
		t.Errorf("after a delivery the endpoint reports %+v, want a 200", items)
	}
	if items[0].LastError != "" {
		t.Errorf("last_error = %q, want empty", items[0].LastError)
	}
	if strings.Contains(body2, "/hook") {
		t.Errorf("the list body contains the webhook path: %s", body2)
	}
}

// docs:start test-delivery-retries

// TestDeliveryRetriesAndCancels: the two failure answers. A 500 is an
// ordinary error, which is how the job system is told to retry; a 404 is
// river.JobCancel, which is how it is told to stop. Neither is a loop in the
// module.
func TestDeliveryRetriesAndCancels(t *testing.T) {
	app := newApp(t, notifications.AllowPrivateTargets(true))
	client, _, orgID := signUp(t, app, "ada@example.com")
	server := newReceiver(t)
	endpoint := register(t, client, orgID, "Kitchen", server.URL+"/hook")
	worker := deliveryWorker(app)
	args := usecase.DeliveryArgs{EndpointID: endpoint.ID, OrgID: orgID, Title: "New order", Lines: []string{"Order ord_7"}}

	// A 500 is transient: an ordinary error, so River retries with its own
	// backoff, and no audit event yet because four attempts are left.
	server.answer(http.StatusInternalServerError)
	err := worker.Work(context.Background(), deliveryJob(args, 1, 5))
	if err == nil {
		t.Fatal("Work after a 500 = nil, want an error the job system would retry")
	}
	if errors.Is(err, &river.JobCancelError{}) {
		t.Errorf("Work after a 500 cancelled the job: %v", err)
	}
	if !errors.Is(err, domain.ErrDeliveryFailed) {
		t.Errorf("Work after a 500 = %v, want a transient failure", err)
	}
	if n := auditCount(t, app, usecase.ActionFailed, orgID); n != 0 {
		t.Errorf("%s events after attempt 1 of 5 = %d, want 0: a retried delivery must not write an event per attempt", usecase.ActionFailed, n)
	}

	// The same failure on the last attempt is worth recording once.
	if err := worker.Work(context.Background(), deliveryJob(args, 5, 5)); err == nil {
		t.Fatal("Work on the last attempt = nil, want the failure")
	}
	if n := auditCount(t, app, usecase.ActionFailed, orgID); n != 1 {
		t.Errorf("%s events after the last attempt = %d, want 1", usecase.ActionFailed, n)
	}

	// The restaurant can see why, without the URL.
	items, _ := list(t, client, orgID)
	if len(items) != 1 || items[0].LastStatus != http.StatusInternalServerError || items[0].LastError == "" {
		t.Fatalf("after failures the endpoint reports %+v, want a 500 and a reason", items)
	}
	if strings.Contains(items[0].LastError, "/hook") {
		t.Errorf("last_error contains the webhook path: %q", items[0].LastError)
	}

	// A 404 will never come right: the job is cancelled, whatever attempt it
	// is on.
	server.answer(http.StatusNotFound)
	err = worker.Work(context.Background(), deliveryJob(args, 1, 5))
	if !errors.Is(err, &river.JobCancelError{}) {
		t.Fatalf("Work after a 404 = %v, want a cancelled job", err)
	}
	if !errors.Is(err, domain.ErrDeliveryRejected) {
		t.Errorf("Work after a 404 = %v, want a permanent rejection", err)
	}

	// An endpoint the restaurant removed is cancelled too, not retried.
	client.Delete(collection(orgID)+"/"+endpoint.ID).AssertStatus(t, http.StatusNoContent)
	err = worker.Work(context.Background(), deliveryJob(args, 1, 5))
	if !errors.Is(err, &river.JobCancelError{}) || !errors.Is(err, domain.ErrEndpointNotFound) {
		t.Fatalf("Work for a removed endpoint = %v, want a cancelled job", err)
	}
}

// docs:end test-delivery-retries

// TestFanoutEnqueuesOneDeliveryPerEndpoint: the public notify API puts one
// fanout job in the queue, and the fanout worker turns it into one delivery
// job per endpoint — which is what lets a broken channel be retried on its
// own.
func TestFanoutEnqueuesOneDeliveryPerEndpoint(t *testing.T) {
	app := newApp(t, notifications.AllowPrivateTargets(true))
	client, _, orgID := signUp(t, app, "ada@example.com")
	kitchen := register(t, client, orgID, "Kitchen", "https://hooks.example.com/services/T0/B0/KITCHEN")
	front := register(t, client, orgID, "Front of house", "https://hooks.example.com/services/T0/B0/FRONT")

	ctx := context.Background()
	notify := notifications.With(app.App().Deps().Jobs)
	if err := notify(ctx, orgID, "New order", []string{"Order ord_7"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	queued := app.Jobs(t, notifications.FanoutJob)
	if len(queued) != 1 {
		t.Fatalf("%s jobs = %d, want 1", notifications.FanoutJob, len(queued))
	}
	if args := string(queued[0].Args); !strings.Contains(args, orgID) {
		t.Errorf("fanout args = %s, want the organisation", args)
	}

	// Workers don't run in tests, so the fanout job just queued is worked by
	// hand, with the app's own job client passed in: in production the worker
	// takes River's from its context, which a test has no way to build.
	worker := usecase.NewFanoutWorker(repository.NewStore(app.App().Deps().DB), app.App().Deps().Jobs, nil)
	fanout := &river.Job[usecase.FanoutArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: usecase.FanoutJob, Attempt: 1, MaxAttempts: 3},
		Args:   usecase.FanoutArgs{OrgID: orgID, Title: "New order", Lines: []string{"Order ord_7"}},
	}
	if err := worker.Work(ctx, fanout); err != nil {
		t.Fatalf("fanout Work = %v", err)
	}

	deliveries := app.Jobs(t, notifications.DeliveryJob)
	if len(deliveries) != 2 {
		t.Fatalf("%s jobs = %d, want one per endpoint", notifications.DeliveryJob, len(deliveries))
	}
	got := map[string]bool{}
	for _, job := range deliveries {
		var args usecase.DeliveryArgs
		if err := json.Unmarshal(job.Args, &args); err != nil {
			t.Fatalf("decode delivery args %s: %v", job.Args, err)
		}
		got[args.EndpointID] = true
		if args.OrgID != orgID {
			t.Errorf("delivery args org = %q, want %q", args.OrgID, orgID)
		}
		if strings.Contains(string(job.Args), "KITCHEN") || strings.Contains(string(job.Args), "FRONT") {
			t.Errorf("the job arguments carry the webhook URL: %s", job.Args)
		}
	}
	if !got[kitchen.ID] || !got[front.ID] {
		t.Errorf("delivery jobs = %v, want one for %s and one for %s", got, kitchen.ID, front.ID)
	}
}

// docs:start test-sender-refuses-to-dial

// TestStrictSenderRefusesToDial: the guard that actually holds.
//
// The URL here never goes through domain.ParseURL — RestoreURL is how a
// stored row comes back, and it applies no policy — so the registration
// check is out of the picture entirely. The strict sender still never
// reaches the server, because the refusal happens in the dialler's Control
// function, on the address about to be connected to. That is what survives a
// host name that resolves somewhere else later, and it is why the check at
// registration time is a convenience rather than the protection.
func TestStrictSenderRefusesToDial(t *testing.T) {
	server := newReceiver(t)

	strict := notifications.NewSender(domain.Policy{})
	target := domain.RestoreURL(server.URL + "/hook")
	status, err := strict.Send(context.Background(), target, domain.Message{Title: "New order"})

	if !errors.Is(err, domain.ErrForbiddenTarget) {
		t.Fatalf("Send = (%d, %v), want the dialler to refuse the address", status, err)
	}
	if !errors.Is(err, domain.ErrDeliveryRejected) {
		t.Errorf("Send = %v, want a permanent rejection: a forbidden address will not become allowed", err)
	}
	if status != 0 {
		t.Errorf("status = %d, want 0: nothing answered", status)
	}
	if strings.Contains(err.Error(), "/hook") {
		t.Errorf("the error contains the URL path: %v", err)
	}
	if posted := server.posted(); len(posted) != 0 {
		t.Fatalf("the server received %d requests; the guard did not stop it", len(posted))
	}

	// The permissive sender, which is what development and these tests run,
	// reaches the same address happily. The policy is the only difference.
	loose := notifications.NewSender(domain.Policy{AllowPrivateTargets: true})
	if status, err := loose.Send(context.Background(), target, domain.Message{Title: "New order"}); err != nil || status != http.StatusOK {
		t.Fatalf("permissive Send = (%d, %v), want (200, nil)", status, err)
	}
}

// docs:end test-sender-refuses-to-dial
