# 17. Extending the framework

Every chapter so far has used something gorbital brings. This one is about what happens when it doesn't bring it.

Restaurants want to be told when an order arrives, in the place they already look — the kitchen's Slack, the front-of-house Mattermost, whatever their chat is. They register a Slack-style incoming webhook URL and Plateful posts to it.

**gorbital has no outbound webhooks and no notification channel but email.** There is no `NotificationChannel` interface to implement, no delivery machinery to register a transport with, no retry-and-backoff helper for talking to somebody else's server, and nothing that knows what a restaurant's endpoints are. If Plateful wants this, Plateful builds it.

This is the most useful chapter in the tutorial, because it is the one that happens to you. Frameworks run out. What matters is what you do at the edge: how much you build, how much you reuse, and which parts of the safety net you now have to weave yourself.

## 1. First, decide where it belongs

**What we're doing.** Asking whether this is app code or framework code, before writing any.

**Why.** Both answers are expensive in different ways, and the question is easier to answer now than after three hundred lines exist.

Four questions, in order:

**Is it already there under another name?** Search before you build. gorbital has jobs, mail, storage, audit, settings, flags, rate limits, idempotency and webhook *verification* — and it is the last one that catches people, because "webhooks" appears in the framework and means receiving, not sending. `gorbital.dev/webhook` verifies deliveries arriving at your app ([chapter 15](15-payments-a-rule-across-modules.md)). Nothing sends them.

**Is it about this app's domain?** "A restaurant's alert channels" is Plateful's business. The name of the table, what a message says, who may register one, whether kitchen staff or only owners may see them — every one of those is a decision about restaurants. That is an app module, and always will be.

**Is it a general mechanism this app happens to need first?** "Deliver a JSON body to a customer-supplied URL with retries" is not about restaurants at all. It could be a framework feature. That is the honest reading of this module, and it is worth writing down: if two more apps need it, it is a library, and the module below is the prototype.

**What does it cost to be wrong?** Building it in the app and later extracting it costs a refactor. Building it in the framework first costs an API you are stuck with — and, here, a security-critical API you are stuck with.

So: an app module, written as if it might be extracted, using the framework for everything the framework already does.

That last clause is the whole discipline. Below the HTTP layer this module is the app's own, but it contains **no retry loop, no backoff, no scheduler, no audit plumbing and no permission machinery**, because those exist. What it contains is exactly the thing that is missing.

## 2. The module, and what it reuses

**What we're doing.** Laying out `internal/modules/notifications`.

**Why.** A new module is the unit of "a thing this app does", and gorbital already gives a module a great deal for free.

**What the framework already gives us.** Everything a module gets: error mappings to HTTP problems, a permission catalogue, org-scoped guards, migrations, routes, and job definitions with retries and operator-editable configuration.

**What we build ourselves.** The four layers, plus one file at the module root — `sender.go`, the HTTP adapter — because it talks to the outside world rather than being a use case.

**How.** The permissions are the first place the framework does the work:

<!-- include examples/apps/plateful/internal/modules/notifications/module.go#notifications-permissions -->

**What just happened.** An endpoint's URL is a bearer credential: whoever holds it can post into the restaurant's chat. So neither permission is granted to `member`. Everywhere else in Plateful a member is kitchen staff and can do the day's work; here, reading the list tells you which channels exist and writing it lets you point the restaurant's order alerts at your own webhook. Both belong to the people who run the business. That is one line of configuration, and it is the whole access-control story for the module — nothing to write, nothing to test beyond the guard already tested by the framework.

## 3. The API other modules call

**What we're doing.** Deciding how the orders module asks for a notification.

**Why.** The orders module must not learn how a notification is delivered, and — more importantly — a notification must not exist for an order that rolled back.

**What the framework already gives us.** `jobs.Client.InsertTx`, which writes the job row through a `pgx.Tx`. A worker can pick the job up only once that transaction commits, and a rollback takes the job with it.

**What we build ourselves.** A function that returns job *arguments* rather than doing anything:

<!-- include examples/apps/plateful/internal/modules/notifications/notify.go#notify-api -->

**What just happened.** `Fanout` returns `river.JobArgs` so the caller can insert it in **their** transaction. That is the difference between "the restaurant was told about an order that exists" and "the restaurant was told about an order that rolled back", and it cannot be fixed by a helper that inserts on its own.

The orders module uses it like this, from the transaction manager that [chapter 13](13-background-jobs.md) touched:

<!-- include examples/apps/plateful/internal/modules/orders/repository/tx.go#tx-manager -->

`FromWorker` is the other half, for a caller that is a job rather than a request — `gorbital.Deps.Jobs` is nil while jobs are being defined, so a scheduled worker cannot be handed a client at startup and takes River's own from the context instead. Both are exported from the module's **root** package, which is the only part of one module another module may import.

## 4. Registration, and the security that is now yours

**What we're doing.** `POST /v1/orgs/{orgId}/notification-endpoints`, taking a URL a human typed into a form.

**Why.** This is the point where the module stops being ordinary. An app that dials a URL supplied by a user is a server-side request forgery engine by construction, unless it is built not to be. Plateful runs in a network with a database, an internal metadata service, and whatever else your cloud puts on a link-local address; the restaurant's form field is a request to make this server fetch something on their behalf.

**What the framework already gives us.** The guard, the permission, the validation of the request body's shape, and the problem response. **It does not give us one line of SSRF protection**, and it never will, because "which addresses may this deployment reach" is a deployment question that no library can answer for you.

**What we build ourselves.** A policy, a URL type that can't be printed by accident, and two checks in two different places.

**How.** The policy first — one boolean, and no way to change it at runtime:

<!-- include examples/apps/plateful/internal/modules/notifications/domain/url.go#endpoint-policy -->

Then parsing, at registration:

<!-- include examples/apps/plateful/internal/modules/notifications/domain/url.go#parse-endpoint-url -->

<!-- include examples/apps/plateful/internal/modules/notifications/domain/url.go#endpoint-url-address -->

and the use case that calls it:

<!-- include examples/apps/plateful/internal/modules/notifications/usecase/register_endpoint.go#register-endpoint -->

**What just happened.** Five refusals and one deliberate non-refusal.

`https` only, unless private targets are allowed — the URL travels in the request, and plaintext hands it to anything on the path. No user information, because `https://user:password@host` is a second credential this app has no business carrying. No fragment, because it is never sent to the server and its only use here is hiding something from a reader. A length bound, because a registration field with no bound is a way to store arbitrary data. And no literal private, loopback, link-local, multicast, unspecified or carrier-grade-NAT address.

The non-refusal: **a host name is deliberately not resolved here.** An answer now says nothing about what the name will resolve to when the delivery job runs — that is DNS rebinding, and it costs an attacker nothing — and resolving it would only add a lookup whose timing the attacker controls. What this check buys is a 422 at registration with a message a restaurateur can act on, instead of a delivery that silently never works. It is a convenience. The protection is in step 5.

Two things about the policy itself are worth copying.

**It is a constructor argument, not a setting.** `main.go` passes `notifications.AllowPrivateTargets(os.Getenv("APP_ENV") != "production")`:

<!-- include examples/apps/plateful/cmd/api/signin.go#webhook-targets -->

A runtime setting would let an operator turn the SSRF guard off from `/ops` at three in the morning, on the word of whoever is shouting loudest. A module that read the environment itself would hide a security decision inside a library function. A module that needs configuring declares `func Module(opts ...Option)` and `main.go` wires it by hand — which is why it is left out of the generated `modules.gen.go`, whose entries are called with no arguments.

**The URL type refuses to print itself.** `domain.URL` keeps the whole URL in an unexported field; `String()` returns `https://hooks.example.com/...`; `Secret()` is the single named way to get the real thing, and the module's sender is its only caller. The safe rendering is the default one, because the dangerous one only has to escape once — a stray `%v` in a log line, a struct handed to a JSON encoder, a reflection-based logger walking a value.

## 5. The guard that actually holds

**What we're doing.** Building the HTTP client deliveries are posted with.

**Why.** Because the check in step 4 proves nothing about the socket that eventually opens.

**What the framework already gives us.** Nothing. `http.DefaultClient` would follow redirects, use `HTTPS_PROXY` from the environment, pool connections and wait as long as the other end likes.

**What we build ourselves.** Every line of it, and every line is a defence:

<!-- include examples/apps/plateful/internal/modules/notifications/sender.go#delivery-http-client -->

**What just happened.** Five decisions, each closing a door:

**`Control` on the dialler.** It runs *after* DNS, on the address about to be connected to, every time. A host name that resolved to a public address at registration can resolve to `127.0.0.1` when the job runs; this check sees the second address, and a check made anywhere else does not.

**No proxy, not even from the environment.** A proxy opens the connection on our behalf, so `Control` would see the proxy's address and never the endpoint's. `HTTPS_PROXY` in a deployment's environment would quietly disable the guard above.

**No keep-alives.** A pooled connection is reused for a later job on the same host **without `Control` running again** — the same rebinding hole through a different door. The cost is one handshake on a job that runs once per order.

**Redirects are not followed.** A redirect is how an attacker turns a host that passed every check into one that would not have: register `https://public.example.com/hook`, answer `302` to `http://169.254.169.254/`, and the guard never sees the second address because Go's client dials it as a new request.

**Timeouts at every stage.** Dial, TLS handshake, response headers, and the whole request. A target that accepts a connection and then says nothing is the cheapest way there is to tie up a worker queue.

That fourth one has a detail worth pausing on. `CheckRedirect` returns `http.ErrUseLastResponse` rather than an error of its own — and the reason is not style:

> **Don't do this.** `return errors.New("redirects are not followed")`.
>
> Go wraps a `CheckRedirect` error in a `*url.Error`, and **`*url.Error`'s message quotes the whole URL**: `Post "https://hooks.example.com/services/T0/B0/XXXXXXXX": redirects are not followed`. That string then goes wherever the error goes — the job's error history in `/ops/jobs/runs`, the worker's log line, and the endpoint's `last_error` column, which the API returns to anyone with `notifications.endpoint.read`. One convenience has published a live credential in three places.

> **Do this instead.** `ErrUseLastResponse` hands the 3xx back as an ordinary response, which the sender classifies as permanent and whose body it can close. No error, no `*url.Error`, no URL.

## 6. Sending, and never wrapping a transport error

<!-- include examples/apps/plateful/internal/modules/notifications/sender.go#send-notification -->

**What just happened.** The same trap as above, in its general form. Every error the transport produces is a `*url.Error` whose message is `Post "<the whole URL>": …`.

> **Don't do this.**
>
> ```go
> resp, err := s.client.Do(req)
> if err != nil {
>     return 0, fmt.Errorf("delivering to endpoint: %w", err) // the URL is now in the error
> }
> ```

> **Do this instead.** Classify the error and build a new one that names the **host** and the kind of failure, and drop the original. That is what `transportError` does: `errors.Is(err, domain.ErrForbiddenTarget)` for the dialler's refusal, `context.DeadlineExceeded`, `context.Canceled`, and everything else as "could not be reached".

The same care applies to three other error sources in the same function, and each is commented where it happens: `json.Marshal` quotes the value it choked on, `http.NewRequestWithContext` quotes the URL, and `url.Parse` (back in step 4) quotes what it could not parse. None of them is wrapped.

The status classification is the other half of the function, and it is what turns an HTTP answer into an instruction for the job system: 2xx is success; 408, 429 and 5xx wrap `ErrDeliveryFailed` because the endpoint is busy rather than wrong; everything else — a 404 for a deleted webhook, a 403 for a revoked one, a 3xx that means it tried to redirect us — wraps `ErrDeliveryRejected`, because no number of attempts will change it.

## 7. Two jobs, and no retry loop anywhere

**What we're doing.** Delivering, with retries, without writing retries.

**Why.** Talking to someone else's server fails constantly and mostly transiently. This is exactly the work [chapter 13's](13-background-jobs.md) job system exists for.

**What the framework already gives us.** The retry loop, its backoff, the attempt counter, the per-attempt timeout, the error history, and an operator's ability to change `max_attempts` during an incident without a deploy.

**What we build ourselves.** Two definitions and two `Work` methods that return errors:

<!-- include examples/apps/plateful/internal/modules/notifications/module.go#notifications-jobs -->

**What just happened.** The split into two jobs is the design decision, and it is what makes the retries honest. A restaurant with three channels gets one fanout job and three delivery jobs, each with its own attempt count and its own backoff — so a channel whose server is down is retried on its own, and the two that worked are never posted to twice.

<!-- include examples/apps/plateful/internal/modules/notifications/usecase/fanout_job.go#fanout-work -->

The fanout is deliberately at-least-once: if the fifth insert fails, the attempt returns an error and the retry enqueues all five again, so an endpoint may be told twice. For a notification that is the right way round — a duplicate alert is a nuisance, a missed order is a lost dinner — and the alternative, remembering how far it got, is exactly the hand-rolled bookkeeping the job system exists to avoid.

Then the delivery, which is where the three answers from [chapter 13](13-background-jobs.md) do all the work:

<!-- include examples/apps/plateful/internal/modules/notifications/usecase/delivery_job.go#delivery-work -->

> **Don't do this.** A retry loop in the worker:
>
> ```go
> for attempt := range 5 {
>     if status, err := w.sender.Send(ctx, e.URL, msg); err == nil {
>         return nil
>     }
>     time.Sleep(backoff(attempt))
> }
> ```
>
> It holds a worker slot for minutes, loses everything on a deploy, is invisible in the operator's job list, and the number 5 can only be changed by shipping code.

> **Do this instead.** `return err` for transient, `river.JobCancel(err)` for permanent, `nil` for done. **There is no `for` and no `time.Sleep` anywhere in this module.** The retry policy is `MaxAttempts: 5` in a definition an operator can edit.

Two smaller decisions in `Work` are worth stealing.

**The endpoint is read back on every attempt**, not trusted from the job's arguments — so a webhook URL a restaurant rotated, or revoked by deleting the row, takes effect on the next attempt instead of the next order. It is also why `DeliveryArgs` names the endpoint rather than carrying its URL: the secret stays in one database row instead of sitting in the queue table in plain text for as long as job history is kept.

**The audit event on failure is written only on the last attempt.** Five attempts are not five failures; they are one failure described five times, and an audit trail that inflates like that is one nobody reads.

## 8. Audit, on success and on failure

**What we're doing.** Recording what was sent and what wasn't.

**Why.** This module posts a restaurant's data to a third party. "Did Plateful tell us about order 7?" and "who pointed our alerts at that URL?" are questions somebody will ask, and the second one is a security question.

**What the framework already gives us.** `audit.Recorder`, handed to every module as `Deps.Audit`, which fills in the actor, the organisation and the request from the context.

**What we build ourselves.** Four action names, and — for the jobs — an actor, because a worker has none:

<!-- include examples/apps/plateful/internal/modules/notifications/usecase/service.go#notification-audit -->

**What just happened.** Nothing called `actor.WithActor` on a worker's context, so `audit.FromContext` would file a delivery under the anonymous actor with no organisation — wrong in the one place an operator most wants the trail to be right. So the job names itself: the actor is the system, identified by the job's own name, and the organisation comes from the job's arguments rather than being inferred.

Three rules hold across all four events. A failed audit write is **logged, not returned** — an audit write that fails must not undo the work it describes. The context is stripped of cancellation with `context.WithoutCancel`, so an event still lands when the request, or the job's timeout, has already ended. And the metadata carries the endpoint's ID, its **host** and the status, and never the URL. [Chapter 18](18-audit-logs-and-observability.md) is where these come back out.

## 9. Testing it against a real server

**What we're doing.** Proving the delivery works, the retries are classified correctly, and the SSRF guard holds — without a network.

**Why.** Every interesting property of this module is about what happens on the wire.

**What the framework already gives us.** [`gorbitaltest`](../guides/testing-with-gorbitaltest.md) for the app, and the module's own `Option` so a test can allow private targets — which is the only reason a test can post to `127.0.0.1` at all.

**What we build ourselves.** An `httptest` server that records what it was sent and answers with whatever status the test chose:

<!-- include examples/apps/plateful/internal/modules/notifications/notifications_test.go#notification-receiver -->

**How.** Workers do not run in tests, so the delivery worker is built by hand and `Work` is called directly, with the attempt number the test wants to pretend to be:

<!-- include examples/apps/plateful/internal/modules/notifications/notifications_test.go#test-delivery-retries -->

**What just happened.** A 500 produced an ordinary error and **no** audit event, because four attempts remained. The same failure on attempt 5 of 5 produced exactly one. A 404 produced a `river.JobCancelError`. A deleted endpoint produced a cancellation too, not a retry. And the endpoint's `last_error`, which the API returns, was asserted not to contain the webhook's path.

The last test is the important one, because it takes the registration check out of the picture entirely:

<!-- include examples/apps/plateful/internal/modules/notifications/notifications_test.go#test-sender-refuses-to-dial -->

`domain.RestoreURL` is how a stored row comes back and applies no policy, so nothing has validated this URL. The strict sender still never reaches the server, because the refusal happens in the dialler's `Control` function on the address about to be connected to. That is what survives a host name resolving somewhere else later — and it is the assertion that would fail if somebody "simplified" the client in step 5 back to `http.DefaultClient`.

## What extending the framework cost

Worth adding up, because the ratio is the lesson:

| Reused, not written | Written, because it is genuinely absent |
|---|---|
| Retries, backoff, attempt counting | The target policy and its dial-time enforcement |
| Per-attempt timeouts, operator-editable in `/ops` | A hardened HTTP client |
| Job history, run list, "run now", retry, cancel | The fanout/delivery split |
| Audit recording, actor and request attribution | Classifying an HTTP answer as transient or permanent |
| Org-scoped guards and the permission catalogue | A URL type that will not print itself |
| Error-to-problem mapping, migrations, routes | The table, the message format, the module |

And the part nobody hands you: **the security of an outbound request is entirely yours.** SSRF validation, redirect refusal, timeouts, a bounded body read, and keeping a credential out of every error, log line and response. A framework can give you retries. It cannot know which addresses your deployment is allowed to reach, and it will not stop you wrapping a `*url.Error`.

## Where to go next

- The job system this module leans on: [background jobs guide](../guides/background-jobs.md).
- The app's other security layers, including the webhooks it *receives*: [security layers guide](../guides/security-layers.md).
- When a module should become a library instead: [services and libraries](../guides/services-and-libraries.md).
- Next chapter: [audit logs and observability](18-audit-logs-and-observability.md), which reads back everything this one recorded.
