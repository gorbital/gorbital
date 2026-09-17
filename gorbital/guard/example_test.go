package guard_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
)

type catalogInput struct {
	ID string `path:"id"`
}

type catalogEntry struct {
	Body struct {
		Title string `json:"title"`
	}
}

func catalogBook(context.Context, *catalogInput) (*catalogEntry, error) {
	out := &catalogEntry{}
	out.Body.Title = "Dune"
	return out, nil
}

// exampleServer mounts routes as a module named books.
func exampleServer(register func(r *gorbital.Router)) server {
	s, err := tryMount(routes(register))
	if err != nil {
		panic(err)
	}
	return s
}

// as sends a GET as the session of a user holding permissions, or
// anonymously when permissions is nil, and returns the status and code.
func (s server) as(target string, principal *auth.Principal, header ...string) string {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	if principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *principal))
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		return fmt.Sprintf("%d %s", rec.Code, problemCode(rec.Body.Bytes()))
	}
	return fmt.Sprint(rec.Code)
}

func ExamplePublic() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", catalogBook)
		gorbital.Get(r, "/v1/wishlist/{id}", catalogBook)
	})
	fmt.Println(s.as("/v1/catalog/bok_1", nil))
	fmt.Println(s.as("/v1/wishlist/bok_1", nil))
	// Output:
	// 200
	// 401 unauthenticated
}

func ExamplePermission() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.Permission("books.book.read"))
	})
	reader := &auth.Principal{UserID: "usr_1", SessionID: "ses_1", Permissions: []string{"books.book.read"}}
	stranger := &auth.Principal{UserID: "usr_2", SessionID: "ses_2"}
	fmt.Println(s.as("/v1/books/bok_1", reader))
	fmt.Println(s.as("/v1/books/bok_1", stranger))
	// Output:
	// 200
	// 403 forbidden
}

func ExampleRecentReauth() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/account/recovery-codes/{id}", catalogBook, guard.RecentReauth())
	})
	justSignedIn := &auth.Principal{UserID: "usr_1", SessionID: "ses_1", SignedInAt: time.Now()}
	signedInYesterday := &auth.Principal{UserID: "usr_1", SessionID: "ses_2", SignedInAt: time.Now().Add(-24 * time.Hour)}
	fmt.Println(s.as("/v1/account/recovery-codes/1", justSignedIn))
	fmt.Println(s.as("/v1/account/recovery-codes/1", signedInYesterday))
	// Output:
	// 200
	// 403 reauthentication_required
}

func ExampleRateLimit() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/exports/{id}", catalogBook, guard.RateLimit(2, time.Hour))
	})
	user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
	for range 3 {
		fmt.Println(s.as("/v1/exports/1", user))
	}
	// Output:
	// 200
	// 200
	// 429 rate_limited
}

func ExampleRateLimitOption() {
	// Options choose what requests are counted under, and whether routes
	// share a budget.
	s := exampleServer(func(r *gorbital.Router) {
		imports := r.Group("/v1/imports", guard.RateLimit(1, time.Hour, guard.ByAPIKey(), guard.Named("imports")))
		gorbital.Get(imports, "/books/{id}", catalogBook)
		gorbital.Get(imports, "/shelves/{id}", catalogBook)
	})
	key := &auth.Principal{UserID: "usr_1", APIKeyID: "key_1"}
	fmt.Println(s.as("/v1/imports/books/1", key))
	fmt.Println(s.as("/v1/imports/shelves/1", key))
	// Output:
	// 200
	// 429 rate_limited
}

func ExampleByUser() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/search/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByUser()))
	})
	ada := &auth.Principal{UserID: "usr_ada", SessionID: "ses_1"}
	grace := &auth.Principal{UserID: "usr_grace", SessionID: "ses_2"}
	fmt.Println(s.as("/v1/search/1", ada))
	fmt.Println(s.as("/v1/search/1", ada))
	fmt.Println(s.as("/v1/search/1", grace))
	// Output:
	// 200
	// 429 rate_limited
	// 200
}

func ExampleByAPIKey() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/sync/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByAPIKey()))
	})
	laptop := &auth.Principal{UserID: "usr_1", APIKeyID: "key_laptop"}
	phone := &auth.Principal{UserID: "usr_1", APIKeyID: "key_phone"}
	fmt.Println(s.as("/v1/sync/1", laptop))
	fmt.Println(s.as("/v1/sync/1", phone))
	// Output:
	// 200
	// 200
}

func ExampleByIP() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByIP()))
	})
	fmt.Println(s.as("/v1/catalog/1", nil))
	fmt.Println(s.as("/v1/catalog/1", nil))
	// Output:
	// 200
	// 429 rate_limited
}

func ExampleNamed() {
	s := exampleServer(func(r *gorbital.Router) {
		writes := guard.RateLimit(1, time.Minute, guard.Named("shelf_writes"))
		gorbital.Get(r, "/v1/shelves/{id}/add", catalogBook, writes)
		gorbital.Get(r, "/v1/shelves/{id}/remove", catalogBook, writes)
	})
	user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
	fmt.Println(s.as("/v1/shelves/1/add", user))
	fmt.Println(s.as("/v1/shelves/1/remove", user))
	// Output:
	// 200
	// 429 rate_limited
}

var ErrSubscriptionRequired = errors.New("books: subscription required")

func ExampleNew() {
	subscribed := guard.New(guard.Spec{
		Name:     "subscription",
		Statuses: []int{http.StatusPaymentRequired},
		Check: func(ctx context.Context, req guard.Request) error {
			a, _ := actor.From(ctx)
			if a.ID != "usr_pro" {
				return ErrSubscriptionRequired
			}
			return nil
		},
	})
	module := routes(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}/audio", catalogBook, subscribed)
	})
	module.Errors = []httpx.Mapping{{Err: ErrSubscriptionRequired, Status: http.StatusPaymentRequired, Code: "subscription_required"}}
	s, err := tryMount(module)
	if err != nil {
		panic(err)
	}
	fmt.Println(s.as("/v1/books/1/audio", &auth.Principal{UserID: "usr_pro", SessionID: "ses_1"}))
	fmt.Println(s.as("/v1/books/1/audio", &auth.Principal{UserID: "usr_free", SessionID: "ses_2"}))
	// Output:
	// 200
	// 402 subscription_required
}

func ExampleSpec() {
	// A guard that checks a path parameter against the caller.
	ownShelf := guard.Spec{
		Name:     "own_shelf",
		Statuses: []int{http.StatusNotFound},
		Check: func(ctx context.Context, req guard.Request) error {
			a, _ := actor.From(ctx)
			if req.PathParam("owner") != a.ID {
				return httpx.NewProblem(http.StatusNotFound, "shelf_not_found", "no shelf of yours has this ID")
			}
			return nil
		},
	}
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/users/{owner}/shelves/{id}", catalogBook, guard.New(ownShelf))
	})
	me := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
	fmt.Println(s.as("/v1/users/usr_1/shelves/1", me))
	fmt.Println(s.as("/v1/users/usr_2/shelves/1", me))
	// Output:
	// 200
	// 404 shelf_not_found
}

// printRequest is a guard that prints what it can read, and allows.
func printRequest(ctx context.Context, req guard.Request) error {
	fmt.Println(req.Operation().OperationID, req.PathParam("id"), req.Query("format"), req.Header("Accept-Language"))
	return nil
}

func ExampleRequest() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: printRequest}))
	})
	s.as("/v1/books/bok_1?format=epub", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}, "Accept-Language", "en")
	// Output:
	// books-get-v1-books-by-id bok_1 epub en
}

func ExampleRequest_PathParam() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
			fmt.Println(req.PathParam("id"))
			return nil
		}}))
	})
	s.as("/v1/books/bok_42", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
	// Output:
	// bok_42
}

func ExampleRequest_Query() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
			fmt.Printf("%q\n", req.Query("preview"))
			return nil
		}}))
	})
	s.as("/v1/books/bok_1?preview=true", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
	// Output:
	// "true"
}

func ExampleRequest_Header() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "client_version", Check: func(_ context.Context, req guard.Request) error {
			if req.Header("X-App-Version") < "2.4.0" {
				return httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated", "update the app to continue")
			}
			return nil
		}}))
	})
	user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
	fmt.Println(s.as("/v1/books/1", user, "X-App-Version", "2.3.0"))
	fmt.Println(s.as("/v1/books/1", user, "X-App-Version", "2.4.1"))
	// Output:
	// 426 app_outdated
	// 200
}

func ExampleRequest_Operation() {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", catalogBook, gorbital.OperationID("books-get"), guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
			fmt.Println(req.Operation().OperationID, req.Operation().Path)
			return nil
		}}))
	})
	s.as("/v1/books/1", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
	// Output:
	// books-get /v1/books/{id}
}
