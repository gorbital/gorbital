# 7. The menu, money and photos

[Chapter 6](06-migrations-and-the-database.md) gave Plateful its tables. This chapter fills two of them, and each teaches one thing that is easy to get wrong and expensive to fix later.

The first is **money**. A price is stored as an integer count of minor units — 950 for £9.50 — and never as a floating-point number. The second is **files**. A restaurant's photos never pass through the API at all: the app hands the client a signed `PUT` URL, the client sends the bytes straight to the bucket, and then comes back and says "done". That shape is not a preference. It is the only shape `storage.Store` supports, and this chapter is honest about what it costs you.

Everything below is in `examples/apps/plateful/internal/modules/menus` and `internal/modules/images`.

---

## 1. One table for a menu

**What we are doing.** Storing a restaurant's dishes so that a menu reads as sections — Starters, Mains, Puddings — with dishes under each.

**Why.** A menu is the busiest read in the app. Every customer looking at a restaurant loads one. Whatever shape we choose here, we pay for on every page view.

**What the framework already gives us.** Nothing, and it shouldn't: this is our data model. gorbital gives us a place to put the migration ([chapter 6](06-migrations-and-the-database.md)) and a repository layer to read it from, and stays out of the modelling.

**What we build ourselves.** One table, `menu_items`, where each dish carries the name of its section and its place within it. There is no `sections` table.

**How.**

<!-- include examples/apps/plateful/db/migrations/20260918010030_menu_items.sql#menu-items-table -->

The grouping happens in one pass over rows the index already returned in the right order:

<!-- include examples/apps/plateful/internal/modules/menus/domain/menu_item.go#group-sections -->

**What just happened.** A section became a fact a dish carries rather than a row that outlives its dishes. Renaming a section, reordering it, or moving a dish between headings are each a single `UPDATE`, and reading a menu is one index scan with no join. The module's package comment says when that decision should be revisited: the day a section grows a photo, a schedule or a description of its own, it has earned a table. It has not yet.

---

## 2. Money is an integer, never a float

**What we are doing.** Choosing the type a price is stored, sent, and added up in.

**Why.** Because `0.10` does not exist in binary floating point. The nearest `float64` to it is slightly more than a tenth. Add ten of them and you do not get `1.00`; you get something that prints as `1.0000000000000002` or rounds inconsistently depending on the order you added in. On one side dish nobody notices. On a bill of twenty lines, a nightly settlement report, or a refund, somebody notices — and the difference between what the customer was charged and what the restaurant is owed is the kind of bug that is found by an accountant, not by a test.

**What the framework already gives us.** Nothing to help and nothing to hinder: gorbital never touches your money. The safety here is entirely in the types you choose, which is why the choice is made once, at the column, and then held all the way out to the JSON.

**What we build ourselves.** A `bigint` column called `price_minor`, an `int64` field, and a JSON field named `price_minor` so no client can mistake it for pounds.

**How.** The column, in the migration above:

```sql
price_minor bigint NOT NULL CHECK (price_minor >= 0),
currency    text   NOT NULL DEFAULT 'GBP' CHECK (char_length(currency) = 3),
```

The request body says the same thing, in the same units, and the `doc` tag tells a client reading the OpenAPI document exactly what the number means:

<!-- include examples/apps/plateful/internal/modules/menus/delivery/create_item.go#create-item-handler -->

And a test proves the property the whole design rests on:

<!-- include examples/apps/plateful/internal/modules/menus/menus_test.go#test-money-is-exact -->

**What just happened.** The price is an `int64` from the request body, through the domain, into a `bigint` column, and back out again — it is never converted to a float anywhere in the process. The currency travels with it, because `950` means nothing on its own. [Chapter 8](08-orders-rules-in-the-domain.md) adds the lines of an order up in the same units, and [chapter 9](09-one-transaction.md) writes them.

> **Don't do this**
>
> ```go
> // A price as a decimal number, in pounds.
> Price float64 `json:"price" example:"9.50"`
> ```
>
> JSON numbers arrive as doubles in most clients, so `9.50` is already slightly wrong before your code sees it, and every total built from it drifts further.
>
> **Do this instead**
>
> ```go
> // A price as an integer count of minor units, with the currency beside it.
> PriceMinor int64  `json:"price_minor" example:"950"`
> Currency   string `json:"currency" example:"GBP"`
> ```
>
> Format it for display at the very edge, where a person reads it, and never do arithmetic on the formatted value.

---

## 3. Adding a dish

**What we are doing.** The `POST` that puts a dish on a restaurant's menu.

**Why.** It is the module's simplest write, which makes it the clearest place to see where each kind of code belongs.

**What the framework already gives us.** Route registration, request parsing and schema validation from the tags on the input struct, the guard that proves the caller is staff of this restaurant, and the error mapping that turns our sentinel errors into `application/problem+json` ([chapter 11](11-validation-and-errors.md)).

**What we build ourselves.** The rules: what a dish may be called, which dietary flags exist, what happens to a price of zero.

**How.** The use case:

<!-- include examples/apps/plateful/internal/modules/menus/usecase/create_item.go#create-item -->

The set of dietary flags is closed, and lives in `domain` rather than in the database:

<!-- include examples/apps/plateful/internal/modules/menus/domain/menu_item.go#dietary-flags -->

**What just happened.** The handler you saw in step 2 read the request and called one function. It priced nothing, checked nothing and decided nothing. Every rule is in `domain` or `usecase`, where a background job, a CLI command or a second route can reach it too.

> **Don't do this**
>
> ```go
> func (h handlers) createItem(ctx context.Context, in *createItemInput) (*menuItemOutput, error) {
>     if in.Body.PriceMinor < 0 {
>         return nil, huma.Error422UnprocessableEntity("price must not be negative")
>     }
>     for _, f := range in.Body.Dietary {
>         if f != "vegan" && f != "vegetarian" { /* … */ }
>     }
>     // …and then the SQL, right here.
> }
> ```
>
> A rule written in a handler exists only for callers who arrive by that route. The importer you write next month, the admin command, the second endpoint that also creates dishes — none of them get it.
>
> **Do this instead**: put the rule in `domain` (what a dish may be) or in `usecase` (what this operation does), and let the handler read the request, call it, and shape the answer. That is what every handler in Plateful does, and it is why they are all four lines long.

---

## 4. Two menus, two kinds of caller

**What we are doing.** Serving the same dishes to two audiences: a restaurant's staff, who see everything, and a customer, who sees what is available right now at a restaurant that is open.

**Why.** They are not the same list, and — more importantly — they are not the same kind of caller. Staff belong to the restaurant's organisation. A customer belongs to no organisation at all.

**What the framework already gives us.** Two guards. `guard.OrgMember(permission)` proves the caller is a member of the organisation in the path *and* that their role holds the permission; anyone else gets `404 org_not_found`, as if the organisation did not exist. `guard.Permission(permission)` only proves the caller is signed in and holds a platform permission.

**What we build ourselves.** The rule about which menu a customer may read, because no guard can express it.

**How.**

<!-- include examples/apps/plateful/internal/modules/menus/delivery/routes.go#menu-routes -->

<!-- include examples/apps/plateful/internal/modules/menus/module.go#menu-permissions -->

<!-- include examples/apps/plateful/internal/modules/menus/usecase/view_menu.go#view-menu -->

**What just happened.** The staff routes are protected by the guard and need nothing else — the organisation is in the query, so another restaurant's dishes simply are not in the result. The customer route is protected by a permission that *every signed-in account holds*, so the guard authorises almost nothing, and the real rule lives in `ViewMenu`. That asymmetry is the single most important thing in this application, and [chapter 10](10-who-may-see-this-row.md) is about nothing else.

The staff list is paginated with a keyset cursor, and the repository keeps one fixed query per sort so nothing a request sends ever becomes SQL text:

<!-- include examples/apps/plateful/internal/modules/menus/usecase/list_items.go#list-items -->

<!-- include examples/apps/plateful/internal/modules/menus/repository/select_items.go#select-items-sql -->

---

## 5. Photos: why an upload is three requests

**What we are doing.** Letting a restaurant put a cover photo and dish photos on its page.

**Why.** Because a menu with no pictures does not sell food, and because file upload is where most APIs quietly acquire a large, slow, memory-hungry endpoint.

**What the framework already gives us.** `gorbital.Deps.Storage` — a `storage.Store`, backed by a local directory in development and by S3, R2, Spaces or MinIO in production ([the storage guide](../guides/storage.md), [`modules/storage`](../methods/modules-storage.md)). Its whole surface for handing work to a client is one method:

```go
// SignedURL returns a URL that lets its holder GET (download) or PUT
// (upload) the key until expiry, without other credentials.
SignedURL(ctx context.Context, key string, method string, expiry time.Duration) (string, error)
```

That is `GET` and `PUT`, and nothing else. There is **no signed `POST`**, so there is no browser upload form to build; and there is **no multipart helper** anywhere in the package, so there is nothing to lean on if you wanted the bytes to come through the app instead. Whatever you build, you build on those two verbs.

**What we build ourselves.** Three routes: ask for somewhere to put the file, then (from the client, not from us) put it there, then tell us to go and look.

**How.**

<!-- include examples/apps/plateful/internal/modules/images/delivery/routes.go#image-routes -->

**What just happened.** The bytes never touch the API server. That is worth counting: a 4 MB photo uploaded through an app is 4 MB read into the app's memory or spooled to its disk, 4 MB written out to the bucket, one request occupying one connection for the length of a slow mobile upload, and a request body limit that has to be raised for everyone. Uploaded directly, it is a JSON request, a `PUT` the app never sees, and a second JSON request.

---

## 6. Step one: ask for somewhere to put it

**What we are doing.** Creating a pending image row and handing back a URL that accepts exactly one `PUT`.

**Why.** The row has to exist first, because the row is what gives the object its key, and the key is what the signature covers.

**What the framework already gives us.** `SignedURL`, and the guard that has already proved the caller is a member of the organisation in the path.

**What we build ourselves.** The key. This is the part that keeps one restaurant out of another's files, so it is worth reading slowly.

**How.**

<!-- include examples/apps/plateful/internal/modules/images/domain/image.go#image-storage-key -->

<!-- include examples/apps/plateful/internal/modules/images/usecase/request_upload.go#request-upload-signed-put -->

<!-- include examples/apps/plateful/internal/modules/images/delivery/request_upload.go#request-upload-handler -->

**What just happened.** The key is `orgs/{orgID}/images/{imageID}.{ext}`, built from the organisation `guard.OrgMember` proved membership of and an unguessable random ID. Nothing the client sent reached it. A request from Bob's Bistro cannot produce a key under Ada's Diner's prefix, whatever it puts in the body, because the body is not consulted — and the signature covers that one key, so a URL for one object cannot be pointed at another.

> **Don't do this**
>
> ```go
> key := "uploads/" + in.Body.Filename   // the client chose the key
> url, _ := bucket.SignedURL(ctx, key, http.MethodPut, time.Hour)
> ```
>
> A client that sends `../../orgs/other-org/images/cover.png` — or simply a filename another tenant also used — is now writing into somebody else's data. And a signed URL is a bearer token: whoever holds it can write that object with no other credential.
>
> **Do this instead**: derive the key from the tenant the guard proved, plus an ID you generated. Never concatenate anything the caller sent into it.

---

## 7. Step three: confirm, and the honest wrinkle

**What we are doing.** Being told the file has arrived, looking at it, and recording what is actually there.

**Why.** Because the app never saw the bytes, it knows nothing about them until it asks.

**What the framework already gives us.** `storage.Store.Stat`, which describes an object without downloading it, and `storage.ErrNotFound` when there is no object at all.

**What we build ourselves.** The checks a normal upload endpoint would do *at the door* — is it too big, is it an image — because with a signed `PUT` there is no door.

**How.** The limit is a runtime setting, changeable in `/ops/settings` without a deploy ([the runtime settings guide](../guides/runtime-settings.md)):

<!-- include examples/apps/plateful/internal/modules/images/module.go#image-settings -->

<!-- include examples/apps/plateful/internal/modules/images/usecase/confirm_upload.go#confirm-upload-stat -->

**What just happened — and what it costs.** This is the part that other write-ups skip, so here it is plainly.

A signed `PUT` URL carries three things: which key may be written, by which method, and until when. It carries **no maximum content length** and **no required `Content-Type`**, because `SignedURL` takes neither. So `images.max_bytes` cannot be enforced while the upload is happening. A client that ignores the documented limit uploads a 40 MB file perfectly successfully, and is refused *afterwards*:

<!-- include examples/apps/plateful/internal/modules/images/images_test.go#test-oversized-upload -->

The consequences you accept by choosing this shape:

- **The bytes are already in the bucket when you refuse them.** The image row stays `pending`, so the client can replace the object and confirm again; `DELETE` removes both the row and the object. An app that must not pay for abandoned bytes needs a sweep job for pending images older than the upload expiry ([chapter 13](13-background-jobs.md) has the shape).
- **The content type is a claim, not a fact.** It is whatever the uploader put in its `PUT`, kept by the store as metadata. Nothing in gorbital sniffs the bytes. If you need to be certain, read the object back and inspect its magic bytes yourself — the module says so in its own comments rather than implying the check is stronger than it is.
- **The limit that matters is the one in force at confirmation**, not at the request, because that is the moment there is something to measure.

---

## 8. Showing the photo back

**What we are doing.** Giving the client a URL it can put in an `<img>` tag.

**Why.** The bucket is private. It has to be — its keys name organisations.

**What the framework already gives us.** The same `SignedURL`, with `GET` this time, and `storage.MaxSignedURLExpiry` (seven days, as S3 allows) as the ceiling.

**What we build ourselves.** A much shorter expiry, and the check that this image belongs to this organisation.

**How.**

<!-- include examples/apps/plateful/internal/modules/images/usecase/get_image.go#signed-get-url -->

**What just happened.** Every read signs a fresh URL that dies in five minutes. A signed URL is a bearer token for one object: anyone who has it can download that object with no account and no session, so the answer to "what if it leaks" is "it stops working almost immediately". Seven days would be available; a page that renders in seconds has no use for it.

Who may ask at all is the module's permissions, which are organisation permissions — every route here sits under `/v1/orgs/{orgId}/`:

<!-- include examples/apps/plateful/internal/modules/images/module.go#image-permissions -->

---

## 9. The body-limit trap in development

**What we are doing.** Setting `APP_MAX_BODY_BYTES` high enough that local uploads work.

**Why.** This one costs people an afternoon, so it is worth its own step.

**What the framework already gives us.** A request body limit applied to every request, and a local storage driver that serves its own signed URLs — from the app's own mux, under `/storage/`.

**What we build ourselves.** Nothing. We just have to notice the interaction.

**How.**

<!-- include examples/apps/plateful/.env.example#max-body-bytes -->

**What just happened.** In development, the "direct to the bucket" upload is direct to *this app*, because the local driver is this app. So the photo travels through the app's body-limit middleware and can be refused with `413` before the images module is ever reached — by a limit that has nothing to do with `images.max_bytes` and does not exist at all in production, where the client `PUT`s to S3. Keep `APP_MAX_BODY_BYTES` above `images.max_bytes` and the two agree. The module's own tests raise it for exactly this reason.

---

## 10. The whole flow, in one test

**What we are doing.** Driving all three steps through the real middleware stack, with a real account, a real organisation and a real upload.

**Why.** A flow with this many moving parts is worth one test that does the whole thing, so a change that breaks the middle of it fails loudly.

**What the framework already gives us.** `gorbitaltest`: a fresh database per test, real sign-in, and a client that speaks to the app's own handler ([testing with gorbitaltest](../guides/testing-with-gorbitaltest.md)).

**What we build ourselves.** A small helper that replays the signed URL against the app, since the test app listens on no socket.

**How.**

<!-- include examples/apps/plateful/internal/modules/images/images_test.go#test-upload-flow -->

**What just happened.** Request an upload; check the key is derived from the organisation and the media type; confirm before uploading and get `409 image_not_uploaded` rather than a 404, because the image exists and the file does not; `PUT` the bytes; confirm; read back a signed download URL and fetch the file through it; delete, and watch both the row and the object go. Every step of the audit trail names the member and the organisation.

---

## What to take from this chapter

- **Money is an integer count of minor units, with its currency beside it, from the column to the JSON.** Format it only where a person reads it.
- **Uploads are signed `PUT` URLs**, because `storage.Store` signs `GET` and `PUT` and nothing else. There is no signed `POST`, no upload form and no multipart helper.
- **Derive the object key from the tenant the guard proved.** Never from the request.
- **Size and type can only be checked after the bytes land**, so say so in your API description, enforce it at confirmation, and have a plan for the bytes you refuse.
- **In development the local driver is the app**, so `APP_MAX_BODY_BYTES` bounds your uploads too.

[Chapter 8](08-orders-rules-in-the-domain.md) turns a basket of these dishes into an order, and puts the rules about what may happen to it in one readable place.
