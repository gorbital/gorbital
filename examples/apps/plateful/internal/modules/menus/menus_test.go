package menus_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/orgshttp"

	"example.com/plateful/db/migrations"
	"example.com/plateful/internal/modules/menus"
	"example.com/plateful/internal/modules/restaurants"
)

// These tests drive the menus routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts. The
// restaurants module is in the app because the customers' route resolves a
// restaurant before it reads a menu, and the only honest way to have an open
// restaurant is for its staff to publish one through that module's API.

// newApp builds the app with sign-in, organisations, the restaurants module
// and the menus module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), restaurants.Module(), menus.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets.
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

// collection is the path of the organisation orgID's menu items.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/menu/items" }

// menuOf is the path of a restaurant's published menu, the customers' route.
func menuOf(restaurantID string) string { return "/v1/restaurants/" + restaurantID + "/menu" }

// apiRestaurant is as much of a restaurant as these tests need.
type apiRestaurant struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
}

// apiItem is a menu item as the API returns it.
type apiItem struct {
	ID         string   `json:"id"`
	Section    string   `json:"section"`
	Position   int      `json:"position"`
	Name       string   `json:"name"`
	PriceMinor int64    `json:"price_minor"`
	Currency   string   `json:"currency"`
	Available  bool     `json:"available"`
	Stock      *int     `json:"stock"`
	Dietary    []string `json:"dietary"`
	CreatedBy  string   `json:"created_by"`
	Version    int64    `json:"version"`
}

// apiItemPage is a page of menu items, the staff's view.
type apiItemPage struct {
	Items      []apiItem `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

// apiMenu is a published menu, the customers' view.
type apiMenu struct {
	Sections []struct {
		Name  string    `json:"name"`
		Items []apiItem `json:"items"`
	} `json:"sections"`
}

// profile is the restaurant profile these tests save, with the version and
// status the caller wants.
func profile(name, status string, version int64) map[string]any {
	return map[string]any{
		"version": version, "name": name, "address": "1 Example Street",
		"cuisine": "Neapolitan", "delivery_radius_m": 3000, "status": status,
		"opens_minute": 660, "closes_minute": 1320,
	}
}

// openRestaurant publishes the organisation's restaurant and returns it,
// open and ready for a customer to read the menu of. A restaurant is created
// onboarding, so opening it is a second save with the version the first
// returned.
func openRestaurant(t *testing.T, client *gorbitaltest.Client, orgID, name string) apiRestaurant {
	t.Helper()
	path := "/v1/orgs/" + orgID + "/restaurant"
	var created apiRestaurant
	res := client.Put(path, profile(name, "onboarding", 0))
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &created)

	var opened apiRestaurant
	res = client.Put(path, profile(name, "open", created.Version))
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &opened)
	if opened.Status != "open" {
		t.Fatalf("restaurant status = %q, want open", opened.Status)
	}
	return opened
}

// pause takes a restaurant off the platform the way its own staff do, for
// the night or for good.
func pause(t *testing.T, client *gorbitaltest.Client, orgID, name string, version int64) {
	t.Helper()
	client.Put("/v1/orgs/"+orgID+"/restaurant", profile(name, "paused", version)).AssertStatus(t, http.StatusOK)
}

// dish is a create request for one menu item.
func dish(section, name string, priceMinor int64) map[string]any {
	return map[string]any{"section": section, "name": name, "price_minor": priceMinor}
}

func TestCreateAndListMenuItems(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")

	res := ada.Post(collection(adaOrg), map[string]any{
		"section": " Starters ", "position": 1, "name": " Bruschetta ",
		"description": "Tomato, basil, olive oil", "price_minor": 650,
		"dietary": []string{"VEGAN", "vegetarian", "vegan"},
	})
	res.AssertStatus(t, http.StatusCreated)
	var created apiItem
	res.JSON(t, &created)
	switch {
	case len(created.ID) < 4 || created.ID[:4] != "mnu_":
		t.Errorf("created ID = %q, want a mnu_ ID", created.ID)
	case created.Section != "Starters" || created.Name != "Bruschetta":
		t.Errorf("created = %+v, want the section and name trimmed", created)
	case created.PriceMinor != 650 || created.Currency != "GBP":
		t.Errorf("created = %+v, want 650 minor units in the default currency", created)
	case !created.Available || created.Stock != nil:
		t.Errorf("created = %+v, want an available dish with unlimited stock", created)
	case created.CreatedBy != adaID || created.Version != 1:
		t.Errorf("created = %+v, want it created by Ada at version 1", created)
	case !slices.Equal(created.Dietary, []string{"vegan", "vegetarian"}):
		t.Errorf("dietary = %v, want the flags deduplicated, lowercased and sorted", created.Dietary)
	}

	ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050)).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaOrg), dish("Mains", "Calzone", 1250)).AssertStatus(t, http.StatusCreated)

	// The whole menu, by name, one page at a time.
	var names []string
	cursor := ""
	for range 4 {
		var p apiItemPage
		ada.Get(collection(adaOrg)+"?limit=2&sort=name&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
		for _, item := range p.Items {
			names = append(names, item.Name)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if want := []string{"Bruschetta", "Calzone", "Margherita"}; !slices.Equal(names, want) {
		t.Errorf("pages sorted by name = %v, want %v", names, want)
	}

	// Sorting by price is sorting by the integer the price is stored as.
	var byPrice apiItemPage
	ada.Get(collection(adaOrg)+"?sort=-price_minor").JSON(t, &byPrice)
	var prices []int64
	for _, item := range byPrice.Items {
		prices = append(prices, item.PriceMinor)
	}
	if want := []int64{1250, 1050, 650}; !slices.Equal(prices, want) {
		t.Errorf("prices sorted descending = %v, want %v", prices, want)
	}

	// A cursor belongs to the sort it came from, and the sort has to be one
	// of the three the module lists.
	var first apiItemPage
	ada.Get(collection(adaOrg)+"?limit=1&sort=name").JSON(t, &first)
	ada.Get(collection(adaOrg)+"?sort=created_at&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection(adaOrg)+"?sort=stock").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	// Filters: one section, and the switch the kitchen uses.
	var mains apiItemPage
	ada.Get(collection(adaOrg)+"?section=mains").JSON(t, &mains)
	if len(mains.Items) != 2 {
		t.Errorf("section=mains = %+v, want the two mains", mains.Items)
	}
}

// TestPriceStaysExactInMinorUnits is the money rule, end to end: a price is
// an integer count of minor units from the request body to the column and
// back, and never a float on the way.
// docs:start test-money-is-exact

func TestPriceStaysExactInMinorUnits(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")

	// 10p, the price that has no exact float: the nearest double to 0.10 is
	// a hair above it.
	res := ada.Post(collection(adaOrg), dish("Sides", "Olives", 10))
	res.AssertStatus(t, http.StatusCreated)
	var olives apiItem
	res.JSON(t, &olives)
	if olives.PriceMinor != 10 {
		t.Fatalf("price = %d, want exactly 10 minor units", olives.PriceMinor)
	}

	var stored apiItem
	ada.Get(collection(adaOrg)+"/"+olives.ID).JSON(t, &stored)
	if stored.PriceMinor != 10 {
		t.Errorf("price read back = %d, want exactly 10 minor units", stored.PriceMinor)
	}

	// Ten of them come to exactly £1.00 as integers, and not as floats. This
	// is the whole reason the column is a bigint: an order adds line prices
	// up, and the total has to be right to the penny.
	var totalMinor int64
	var totalPounds float64
	for range 10 {
		totalMinor += stored.PriceMinor
		totalPounds += float64(stored.PriceMinor) / 100
	}
	if totalMinor != 100 {
		t.Errorf("ten 10p sides = %d minor units, want 100", totalMinor)
	}
	if totalPounds == 1.0 {
		t.Error("ten 0.10 floats added up to exactly 1.00 here, which is not how binary floats behave: the rest of this module's reasoning about money assumes they don't")
	}

	// A price with a fractional part isn't a count of minor units at all.
	ada.Post(collection(adaOrg), dish("Sides", "Bread", 0)).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaOrg), map[string]any{"section": "Sides", "name": "Dip", "price_minor": -1}).
		AssertStatus(t, http.StatusUnprocessableEntity)
}

// docs:end test-money-is-exact

// TestUnavailableDishesAreHiddenFromCustomers checks the switch a kitchen
// flicks when it runs out: the dish keeps its place on the staff's menu and
// leaves the customers'.
func TestUnavailableDishesAreHiddenFromCustomers(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	restaurant := openRestaurant(t, ada, adaOrg, "Ada's Pizzeria")
	carol, _, _ := signUp(t, app, "carol@example.com")

	ada.Post(collection(adaOrg), dish("Starters", "Bruschetta", 650)).AssertStatus(t, http.StatusCreated)
	res := ada.Post(collection(adaOrg), dish("Starters", "Arancini", 700))
	res.AssertStatus(t, http.StatusCreated)
	var arancini apiItem
	res.JSON(t, &arancini)
	ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050)).AssertStatus(t, http.StatusCreated)

	// The kitchen has run out of arancini for the evening.
	res = ada.Patch(collection(adaOrg)+"/"+arancini.ID, map[string]any{"version": arancini.Version, "available": false})
	res.AssertStatus(t, http.StatusOK)

	var menu apiMenu
	carol.Get(menuOf(restaurant.ID)).JSON(t, &menu)
	var published []string
	for _, section := range menu.Sections {
		for _, item := range section.Items {
			published = append(published, section.Name+"/"+item.Name)
		}
	}
	if want := []string{"Mains/Margherita", "Starters/Bruschetta"}; !slices.Equal(published, want) {
		t.Errorf("published menu = %v, want %v: sections in order, and no dish the kitchen switched off", published, want)
	}

	// Staff see it, and can find exactly what they switched off.
	var all apiItemPage
	ada.Get(collection(adaOrg)).JSON(t, &all)
	if len(all.Items) != 3 {
		t.Errorf("staff menu = %+v, want all three dishes", all.Items)
	}
	var off apiItemPage
	ada.Get(collection(adaOrg)+"?available=false").JSON(t, &off)
	if len(off.Items) != 1 || off.Items[0].Name != "Arancini" {
		t.Errorf("available=false = %+v, want the arancini only", off.Items)
	}

	// Switching it back on puts it back on the customers' menu, in its place.
	var current apiItem
	ada.Get(collection(adaOrg)+"/"+arancini.ID).JSON(t, &current)
	ada.Patch(collection(adaOrg)+"/"+arancini.ID, map[string]any{"version": current.Version, "available": true}).
		AssertStatus(t, http.StatusOK)
	carol.Get(menuOf(restaurant.ID)).JSON(t, &menu)
	if len(menu.Sections) != 2 || len(menu.Sections[1].Items) != 2 {
		t.Errorf("published menu after switching the dish back on = %+v, want two starters again", menu.Sections)
	}
}

// TestACustomerReadsAPublishedMenu is the route that exists because
// guard.OrgMember could not serve it: Carol is in no restaurant's
// organisation and reads the menu anyway, while a restaurant that isn't open
// is not there to be read.
func TestACustomerReadsAPublishedMenu(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	restaurant := openRestaurant(t, ada, adaOrg, "Ada's Pizzeria")
	ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050)).AssertStatus(t, http.StatusCreated)

	carol, _, carolOrg := signUp(t, app, "carol@example.com")
	var menu apiMenu
	carol.Get(menuOf(restaurant.ID)).JSON(t, &menu)
	if len(menu.Sections) != 1 || len(menu.Sections[0].Items) != 1 || menu.Sections[0].Items[0].PriceMinor != 1050 {
		t.Fatalf("menu Carol read = %+v, want the one dish at 1050 minor units", menu.Sections)
	}
	// Carol's own organisation is her personal workspace, and it has no
	// menu; being a member of it grants her nothing in Ada's.
	var hers apiItemPage
	carol.Get(collection(carolOrg)).JSON(t, &hers)
	if len(hers.Items) != 0 {
		t.Errorf("Carol's own menu = %+v, want none", hers.Items)
	}

	// Signing in is still required: the permission is held by the "user"
	// role, not by nobody.
	app.Client().Get(menuOf(restaurant.ID)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	carol.Get(menuOf("rst_mfrggzdfmztwq2lkmfrggzdfmy")).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
}

// TestAPausedRestaurantHasNoMenu checks that a restaurant which isn't open
// answers the same 404 as one that never existed, so nobody can tell a
// paused or suspended restaurant from a typo by asking for its menu.
func TestAPausedRestaurantHasNoMenu(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	restaurant := openRestaurant(t, ada, adaOrg, "Ada's Pizzeria")
	ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050)).AssertStatus(t, http.StatusCreated)
	carol, _, _ := signUp(t, app, "carol@example.com")
	carol.Get(menuOf(restaurant.ID)).AssertStatus(t, http.StatusOK)

	pause(t, ada, adaOrg, "Ada's Pizzeria", restaurant.Version)

	carol.Get(menuOf(restaurant.ID)).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	// The dishes are still Ada's, and still there for the night it reopens.
	var all apiItemPage
	ada.Get(collection(adaOrg)).JSON(t, &all)
	if len(all.Items) != 1 {
		t.Errorf("staff menu while paused = %+v, want the dish still there", all.Items)
	}
}

// TestAnotherOrganisationsMemberIsTurnedAway checks deny by default and
// organisation isolation: for Bob, Ada's organisation does not exist.
func TestAnotherOrganisationsMemberIsTurnedAway(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	res := ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050))
	res.AssertStatus(t, http.StatusCreated)
	var margherita apiItem
	res.JSON(t, &margherita)
	item := collection(adaOrg) + "/" + margherita.ID

	app.Client().Get(collection(adaOrg)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"list":                      bob.Get(collection(adaOrg)),
		"create":                    bob.Post(collection(adaOrg), dish("Mains", "Mine now", 1)),
		"get":                       bob.Get(item),
		"update":                    bob.Patch(item, map[string]any{"version": 1, "name": "Mine now"}),
		"delete":                    bob.Delete(item),
		"list in an unknown org":    bob.Get(collection("org_mfrggzdfmztwq2lkmfrggzdfmy")),
		"list with a malformed org": bob.Get(collection("not-an-org")),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}

	// In his own organisation, Ada's dish is simply not one of his.
	bobItem := collection(bobOrg) + "/" + margherita.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "menu_item_not_found")
	bob.Patch(bobItem, map[string]any{"version": 1, "name": "Mine now"}).AssertProblem(t, http.StatusNotFound, "menu_item_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "menu_item_not_found")
}

// TestDishNamesAreUniquePerMenu checks the unique index: one restaurant
// never sells two dishes of the same name, and two restaurants may.
func TestDishNamesAreUniquePerMenu(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050)).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"the same name", dish("Mains", "Margherita", 1100), http.StatusConflict, "menu_item_name_taken"},
		{"the same name in another section", dish("Sides", "Margherita", 400), http.StatusConflict, "menu_item_name_taken"},
		{"the same name in another case", dish("Mains", "MARGHERITA", 1100), http.StatusConflict, "menu_item_name_taken"},
		{"a blank name", dish("Mains", "   ", 1100), http.StatusUnprocessableEntity, "validation_failed"},
		{"a blank section", dish("  ", "Quattro Formaggi", 1100), http.StatusUnprocessableEntity, "validation_failed"},
		{"an unknown dietary flag", map[string]any{"section": "Mains", "name": "Quattro Formaggi", "price_minor": 1100, "dietary": []string{"pescatarian"}}, http.StatusUnprocessableEntity, "validation_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaOrg), tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}

	// Another restaurant's menu is another namespace.
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	bob.Post(collection(bobOrg), dish("Mains", "Margherita", 950)).AssertStatus(t, http.StatusCreated)

	// Renaming a dish onto another dish's name is the same conflict.
	res := ada.Post(collection(adaOrg), dish("Mains", "Calzone", 1250))
	res.AssertStatus(t, http.StatusCreated)
	var calzone apiItem
	res.JSON(t, &calzone)
	ada.Patch(collection(adaOrg)+"/"+calzone.ID, map[string]any{"version": calzone.Version, "name": "margherita"}).
		AssertProblem(t, http.StatusConflict, "menu_item_name_taken")
}

// TestVersionConflicts checks the optimistic concurrency on a dish: two
// members editing the same one can't overwrite each other silently.
func TestVersionConflicts(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	res := ada.Post(collection(adaOrg), dish("Mains", "Margherita", 1050))
	res.AssertStatus(t, http.StatusCreated)
	var margherita apiItem
	res.JSON(t, &margherita)
	item := collection(adaOrg) + "/" + margherita.ID

	ada.Patch(item, map[string]any{"name": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 7, "name": "Too new"}).AssertProblem(t, http.StatusConflict, "menu_item_version_conflict")

	res = ada.Patch(item, map[string]any{"version": margherita.Version, "price_minor": 1150, "stock": 12})
	res.AssertStatus(t, http.StatusOK)
	var updated apiItem
	res.JSON(t, &updated)
	if updated.PriceMinor != 1150 || updated.Stock == nil || *updated.Stock != 12 || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new price, 12 in stock and version 2", updated)
	}

	// The version Ada read a moment ago is stale now, which is the point.
	ada.Patch(item, map[string]any{"version": margherita.Version, "price_minor": 1200}).
		AssertProblem(t, http.StatusConflict, "menu_item_version_conflict")

	// Counting portions stops with unlimited, which is the column's NULL.
	res = ada.Patch(item, map[string]any{"version": updated.Version, "unlimited": true})
	res.AssertStatus(t, http.StatusOK)
	var unlimited apiItem
	res.JSON(t, &unlimited)
	if unlimited.Stock != nil || unlimited.Version != 3 {
		t.Errorf("updated = %+v, want unlimited stock at version 3", unlimited)
	}

	// An update that changes nothing is not a change: the version stands.
	res = ada.Patch(item, map[string]any{"version": unlimited.Version, "price_minor": 1150})
	res.AssertStatus(t, http.StatusOK)
	var unchanged apiItem
	res.JSON(t, &unchanged)
	if unchanged.Version != unlimited.Version {
		t.Errorf("version after an update that changed nothing = %d, want %d", unchanged.Version, unlimited.Version)
	}

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "menu_item_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "menu_item_not_found")
}
