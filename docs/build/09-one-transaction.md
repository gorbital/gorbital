# 9. One transaction

Placing an order changes five things:

1. the order row;
2. its lines;
3. the stock of every limited dish it took;
4. a job that tells the restaurant a new order has arrived;
5. (later, on other operations) the courier's current assignment.

If any one of those can happen without the others, you have a bug that is very hard to see and very embarrassing to explain. This chapter makes all of them one change — **including the background job** — and that last part is the single most useful pattern in this guide. Almost nobody writes it down.

Everything is in `examples/apps/plateful/internal/modules/orders`.

---

## 1. What goes wrong when it is two operations

**What we are doing.** Before any code: naming the failures, so that the shape the rest of the chapter builds is clearly worth its cost.

**Why.** Because "wrap it in a transaction" is advice people follow for database writes and then abandon the moment a queue, a cache or an email is involved — which is exactly where it matters most.

Suppose placing an order is a transaction that writes the order, and then, after it commits, an `InsertJob` call that tells the restaurant.

- The process is killed between the commit and the insert. The order exists; nobody in the kitchen is ever told. The diner waits.
- Now suppose the order is enqueued *first*, and the transaction rolls back because a dish ran out. A worker picks the job up half a second later and announces an order that does not exist. The kitchen starts cooking. The notification quotes an order ID that returns 404.
- The stock is decremented in its own statement, outside the transaction. An order fails validation after the decrement, and the portions are gone with no order to show for it.

The second one is the interesting failure, because it is a *race that usually works*. It passes in development, passes in CI, and shows up in production the week the database is slow.

**What just happened.** We have a rule: anything that must not happen unless the order is committed has to be written by the same transaction, and that includes the job row.

---

## 2. What the framework already gives us

Two things, and they are designed to fit together.

**`postgres.InTx`** runs a function in a transaction:

```go
// InTxWithOptions runs fn in a transaction started with opts. It commits when
// fn returns nil. Otherwise it rolls back and returns fn's error unchanged, so
// callers can match domain errors with [errors.Is]. If fn panics, the
// transaction is rolled back and the panic continues.
func InTxWithOptions(ctx context.Context, db Beginner, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error
```

`InTx` is that with the server's default isolation level ([`modules/postgres`](../methods/modules-postgres.md), [the database guide](../guides/database.md)). Four properties earn their keep:

- **Commit on `nil`, roll back on anything else.** There is no `tx.Commit()` for you to forget.
- **The error comes back unchanged.** Not wrapped, not replaced. That is what lets the caller write `errors.Is(err, domain.ErrItemOutOfStock)` and get `true` — and it is why the module's error mapping ([chapter 11](11-validation-and-errors.md)) still works for errors raised deep inside a transaction. A helper that wrapped the error in "transaction failed: …" would break `errors.Is` for every sentinel in the app.
- **A panic rolls back and keeps panicking.** You do not lose the stack trace, and you do not leave a transaction open.
- **The rollback runs on a context that cannot be cancelled** (`context.WithoutCancel`), so a client that hangs up mid-request still gets its locks released rather than leaving them to the connection's timeout.

**`jobs.Client.InsertTx`** enqueues a job *in a transaction*:

```go
// InsertTx enqueues a job in tx: it becomes visible to workers only if tx
// commits.
func (c *Client) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
```

The job queue is [River](../guides/background-jobs.md), and River's queue is tables in the same PostgreSQL database as your data. That is the whole trick: a job is a row, so a job can be written by your transaction, and a rollback takes it with it. No outbox table, no two-phase commit, no reconciliation script.

**What just happened.** The framework supplies the two halves. What is left for us is to arrange our code so that a use case can use them both without knowing they exist.

---

## 3. The port: what a use case is allowed to ask for

**What we are doing.** Declaring, in the `usecase` package, the interface the transaction has to satisfy.

**Why.** The use case is where the rules are, and it must stay free of pgx, River and SQL — partly so it can be read, mostly so it can be tested. So it declares what it needs and lets `repository` provide it.

**What the framework already gives us.** The layering convention, and an architecture test in the app that fails if `usecase` imports `repository`.

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/usecase/ports.go#orders-transaction-port -->

**What just happened.** Two interfaces, and the relationship between them is the design.

`TxManager` has one method. It is the *only* way a use case starts a transaction, so there is no `BeginTx` anywhere in `usecase` to be misused, and no transaction that can outlive the function that started it — `InTx` hands you a `Tx`, and when your function returns, it is gone.

`Tx` embeds `Store`, so everything you can read outside a transaction you can read inside it, on the same connection. Then it adds the four writes that only make sense inside one — including `Notify`, which is "tell the restaurant". Look at how `Notify` is declared: it takes an organisation, a title and some lines. It says nothing about jobs, queues or River, because the use case does not need to know that a notification is a job. It needs to know that *the job runs only if the transaction commits*, and that is what the comment promises.

---

## 4. The implementation: one transaction, and its jobs

**What we are doing.** Providing that port, in `repository`, where pgx and River are allowed.

**Why.** This is the only file in the module that knows the notification is a job row.

**What the framework already gives us.** `postgres.InTx` and `jobs.Client.InsertTx`, as above.

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/repository/tx.go#tx-manager -->

**What just happened.** Read `TxManager.InTx` closely — it is two statements and every part of them matters. It calls `postgres.InTx` on the pool, and inside that closure it builds a `boundTx` out of three things: a store made by `newStoreOn(tx)`, the `pgx.Tx` itself, and the job client.

`newStoreOn(tx)` builds a **new store bound to the transaction**. That is the rule from `InTx`'s own documentation — *build tx-bound repositories inside fn; never keep tx after fn returns* — and it is what guarantees that every read and write the use case does inside the closure happens on that transaction and not on the pool. A store that held the pool would silently do half the work outside the transaction, which is the hardest version of this bug to find, because it works perfectly until something rolls back.

`boundTx` then carries the same `pgx.Tx` so `Notify` can hand it to `InsertTx`. And `Notify` reaches into another module — but only its root package, `notifications`, never its layers, which is the app's rule about module boundaries (there is a test for it). `notifications.Fanout` returns *job arguments* rather than enqueueing anything, precisely so that the caller's transaction gets to be the thing that inserts them.

---

## 5. The use case: everything, once

**What we are doing.** Placing the order.

**Why.** This is the payoff: one function, one transaction, five writes.

**What the framework already gives us.** The transaction and the job insert, both behind our port; the feature flag (`orders.scheduled_ordering`) and the runtime setting (`orders.max_open_per_restaurant`) read as the operation runs; the actor in the context.

**What we build ourselves.** The order of operations, and the reason for each step.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/usecase/place_order.go#place-order -->

**What just happened.** Step by step, inside `s.tx.InTx`:

- **The restaurant is read and must be accepting.** A guard asked the same question before the handler ran ([chapter 12](12-your-own-function.md)), but the guard's answer could be stale by the time we get here; this one cannot be, because the transaction holds it.
- **Open orders are counted** against a runtime setting, so an operator can raise the kitchen's ceiling on a busy Friday from `/ops/settings` with no deploy ([runtime settings](../guides/runtime-settings.md)).
- **The menu items are read with their rows locked**, and the IDs are sorted first. Sorting gives every transaction the same lock order, which is how two baskets containing the same two dishes cannot deadlock against each other.
- **Prices are copied onto the lines.** From this moment the order carries what the customer chose and was charged — [chapter 8](08-orders-rules-in-the-domain.md) explains why that matters.
- **The domain builds and validates the order**, adding the lines up in integer minor units.
- **The order, its lines and the stock are written.**
- **The notification is enqueued through the same transaction**, as the last thing the closure does.

Two things happen *outside* the closure, deliberately. The default delivery address is read before the transaction starts, because it needs no lock and there is no reason to hold one while reading it. And the audit event is recorded after the transaction has committed, because an audit trail should record what happened, not what was attempted — note that it uses `context.WithoutCancel`, so a client hanging up does not lose the record.

---

## 6. The two writes worth reading

**What we are doing.** Looking at the repository methods the closure calls.

**Why.** Both do something slightly cleverer than "run an UPDATE", and both are the kind of thing that is easy to get subtly wrong.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/repository/insert_order.go#insert-order -->

<!-- include examples/apps/plateful/internal/modules/orders/repository/take_stock.go#take-stock -->

**What just happened.** `InsertOrder` writes the lines with `CopyFrom` — one round trip for a basket of any size rather than one per dish — and both statements run on the transaction, so an order never exists without its lines.

`TakeStock` puts the condition **in the `UPDATE`**, not in Go: the statement above subtracts the quantity only `WHERE … stock IS NOT NULL AND stock >= $3`.

If you read the stock into Go, compare it there, and then write the new value, two transactions can both read "1 left" and both write "0 left", and you have sold the last portion twice. Expressing the check as part of the write means the database decides, and exactly one of them affects a row. `PlaceOrder` also locks the rows, so in this app it is belt and braces — but the belt is the one in the SQL, because it holds even when somebody later adds a code path that forgets the lock.

`RowsAffected() == 0` is then ambiguous — the dish might be unlimited, or it might have run out — which is why a second query tells the two apart.

---

## 7. Why the error must come back unchanged

**What we are doing.** Following one failure from the bottom of the stack to the client.

**Why.** Because this is the part that quietly breaks when people write their own transaction helper.

A customer orders two portions of olives; one is left. Inside the closure, `PlaceOrder` finds `*dish.Stock < item.Quantity` and returns:

```go
return fmt.Errorf("%w: %s", domain.ErrItemOutOfStock, item.ItemID)
```

- `postgres.InTx` rolls back and returns **that error**, unchanged.
- `PlaceOrder` passes it to `storeError`, which checks it against the module's known errors with `errors.Is` and returns it as it is (anything it does not recognise — a driver error, say — is hidden behind a generic message, because driver errors are not API).
- The handler passes it up.
- The app's error mapper matches it with `errors.Is` against `gorbital.Module.Errors` and writes `409 dish_out_of_stock` as `application/problem+json`.

Every link in that chain is `errors.Is`. If any one of them wrapped the error in a new sentinel, or replaced it with `errors.New("transaction failed")`, the client would get a `500` and a log line instead of an answer it can act on.

> **Don't do this**
>
> ```go
> if err := fn(tx); err != nil {
>     _ = tx.Rollback(ctx)
>     return fmt.Errorf("transaction rolled back: %v", err)  // %v, not %w
> }
> ```
>
> `%v` flattens the error to a string. `errors.Is` now returns false for everything, the mapping never matches, and every business refusal inside a transaction becomes a `500`.
>
> **Do this instead**: use `postgres.InTx`, which returns `fn`'s error exactly as it was. If you must add context, use `%w` — and only where the wrapped error is not something a caller needs to match.

---

## 8. Proving it

**What we are doing.** Testing that the writes and the job land together, and that a refused order leaves nothing behind.

**Why.** "It is all one transaction" is a claim about what happens when something fails, and the only way to believe a claim like that is to make it fail.

**What the framework already gives us.** `gorbitaltest` gives a real database per test and `App.Jobs(t, kind)` to read the queued jobs. Workers do not run in tests, so the job row is exactly what there is to see — which is perfect here, because the job row is the thing we care about.

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-one-transaction -->

**What just happened.** The first order succeeded: stock went from three to two, and exactly one fanout job was queued. Then two portions were taken, and a third order for two found none — `409 dish_out_of_stock`. And the assertions that matter are the ones after the failure: the stock did not move, **no job was enqueued**, and the `orders` table holds only the two orders that were accepted. The refused order left no trace anywhere, which is the entire promise of this chapter, checked rather than asserted in prose.

---

## 9. Contexts, and the thing to be careful about

**What we are doing.** Making sure the transaction ends when the request does.

**Why.** A transaction holds locks. A transaction nobody is waiting for any more, still holding locks, is how one abandoned request slows down a whole kitchen.

**What the framework already gives us.** The request's `context.Context` carries the client's cancellation and the route's timeout; pgx respects it, so a cancelled context aborts the query rather than waiting for it. `InTx` rolls back on a context that cannot be cancelled, so the cleanup still runs.

**What we build ourselves.** The discipline of passing `ctx` down every single call — which you can see in `PlaceOrder`: every call inside the closure takes the same `ctx`.

> **Don't do this**
>
> ```go
> err = s.tx.InTx(context.Background(), func(tx Tx) error {  // detached from the request
>     order, err := tx.SelectOrder(context.Background(), id, true)
>     // …
> })
> ```
>
> The customer closed the app twenty seconds ago; this transaction is still holding a row lock on a dish every other order needs, and will until the statement finishes on its own.
>
> **Do this instead**: pass the request's `ctx` everywhere, and reach for `context.WithoutCancel(ctx)` only for work that must finish *because* the request ended — the audit record after a successful commit, and the rollback inside `InTx`. Both of those are in the code above; they are the exception that proves the rule.

---

## What to take from this chapter

- **Anything that must not happen unless the write commits belongs in the transaction** — and with River, that includes background jobs, because a job is a row.
- **`postgres.InTx` commits on `nil`, rolls back on anything else, rolls back on a panic, and returns your error unchanged** so `errors.Is` keeps working all the way to the problem response.
- **Build tx-bound stores inside the closure.** A store holding the pool does half the work outside the transaction.
- **Declare a `TxManager` port in `usecase`** so the rules never import pgx or River, and the transaction cannot outlive the function that started it.
- **Put concurrency conditions in the SQL** (`WHERE … AND stock >= $3`), not in Go.
- **Test the rollback**, not just the happy path: after a refusal, check the row, the side effect, and the job are all absent.

[Chapter 10](10-who-may-see-this-row.md) is the most important chapter in this guide: three kinds of caller, one `orders` table, and the authorisation rule that no route table can express.
