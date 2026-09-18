// Package domain holds the menus module's menu items and their rules. It
// imports only the standard library.
package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxSectionLength     = 60
	MaxNameLength        = 100
	MaxDescriptionLength = 1000
	// CurrencyLength is the length of an ISO 4217 code.
	CurrencyLength = 3
	// DefaultCurrency is what an item is priced in when the request says
	// nothing, and what the column defaults to.
	DefaultCurrency = "GBP"
)

// docs:start dietary-flags

// Dietary flags a kitchen may put on a dish. The set is closed and lives
// here rather than in the database, because it is a rule about what a dish
// may claim, and customers filter on exactly these strings: a free-text
// column would fill with "Vegan", "vegan " and "v" and none of them would
// match. Flag names are public API.
const (
	DietaryVegetarian = "vegetarian"
	DietaryVegan      = "vegan"
	DietaryGlutenFree = "gluten_free"
	DietaryHalal      = "halal"
	DietaryNutFree    = "nut_free"
)

// DietaryFlags is every flag a dish may carry, in the order the API
// documents them.
var DietaryFlags = []string{DietaryVegetarian, DietaryVegan, DietaryGlutenFree, DietaryHalal, DietaryNutFree}

// CleanDietary returns flags deduplicated and sorted, so the stored value is
// canonical and two dishes with the same claims hold the same array. It
// returns the unknown flags it found, which the caller reports as a
// validation error.
func CleanDietary(flags []string) ([]string, []string) {
	var unknown []string
	clean := make([]string, 0, len(flags))
	for _, f := range flags {
		f = strings.ToLower(strings.TrimSpace(f))
		switch {
		case f == "":
			continue
		case !slices.Contains(DietaryFlags, f):
			if !slices.Contains(unknown, f) {
				unknown = append(unknown, f)
			}
		case !slices.Contains(clean, f):
			clean = append(clean, f)
		}
	}
	slices.Sort(clean)
	return clean, unknown
}

// docs:end dietary-flags

// An Item is one dish on a restaurant's menu.
type Item struct {
	ID    string
	OrgID string
	// CreatedBy is the member who added the dish, for display and audit.
	// Access comes only from membership.
	CreatedBy string
	ItemFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ItemFields are the fields a restaurant's staff set.
type ItemFields struct {
	// Section is the heading the dish is printed under. Items are grouped by
	// it when the menu is read; there is no separate sections table.
	Section string
	// Position orders the dish inside its section, smallest first, with the
	// ID breaking ties.
	Position    int
	Name        string
	Description string
	// PriceMinor is the price in integer minor units: 950 is £9.50. Money is
	// never a float here. A float cannot represent 0.10 exactly, so ten 10p
	// sides would not add up to £1.00 and the error would grow with every
	// line and every rounding; an integer count of pennies adds up exactly.
	// int64 matches the column's bigint.
	PriceMinor int64
	// Currency is the ISO 4217 code PriceMinor counts the minor units of,
	// which decides how many of them make a unit.
	Currency string
	// Available is the switch a kitchen flicks when it runs out for the
	// evening: the dish stays on the staff's menu and leaves the customers'.
	Available bool
	// Stock nil means unlimited: the kitchen cooks the dish to order and
	// never counts. A number is limited stock, which the orders module
	// decrements in SQL inside the transaction that places the order.
	Stock *int
	// Dietary is the canonical, sorted set of flags from DietaryFlags.
	Dietary []string
	// PhotoImageID is the images module's ID of the dish's photo, empty when
	// it has none.
	PhotoImageID string
}

// A Section is one heading of a menu with the dishes printed under it, in
// their own order.
type Section struct {
	Name  string
	Items []Item
}

// NewItem returns a new menu item of orgID created by createdBy, or a
// *ValidationError.
func NewItem(id, orgID, createdBy string, f ItemFields, now time.Time) (Item, error) {
	item := Item{
		ID: id, OrgID: orgID, CreatedBy: createdBy,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	fields, err := f.clean()
	item.ItemFields = fields
	if err != nil {
		return Item{}, err
	}
	if err := item.validate(); err != nil {
		return Item{}, err
	}
	return item, nil
}

// Changes are the fields an update sets; a nil pointer leaves the field as
// it is. Stock is two fields because nil has a meaning of its own: SetStock
// says the stock is being changed, and Stock nil then means unlimited.
type Changes struct {
	Section      *string
	Position     *int
	Name         *string
	Description  *string
	PriceMinor   *int64
	Currency     *string
	Available    *bool
	SetStock     bool
	Stock        *int
	Dietary      *[]string
	PhotoImageID *string
}

// Apply returns the item with c applied and the names of the fields that
// changed, or a *ValidationError. UpdatedAt becomes now only when something
// changed; the repository increments the version when it saves.
func (i Item) Apply(c Changes, now time.Time) (Item, []string, error) {
	next := i
	setIf(&next.Section, c.Section)
	setIf(&next.Position, c.Position)
	setIf(&next.Name, c.Name)
	setIf(&next.Description, c.Description)
	setIf(&next.PriceMinor, c.PriceMinor)
	setIf(&next.Currency, c.Currency)
	setIf(&next.Available, c.Available)
	setIf(&next.PhotoImageID, c.PhotoImageID)
	if c.SetStock {
		next.Stock = c.Stock
	}
	if c.Dietary != nil {
		next.Dietary = *c.Dietary
	}
	fields, err := next.clean()
	next.ItemFields = fields
	if err != nil {
		return i, nil, err
	}
	if err := next.validate(); err != nil {
		return i, nil, err
	}
	changed := i.changedFields(next)
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

// setIf writes *from to *to when from isn't nil.
func setIf[T any](to *T, from *T) {
	if from != nil {
		*to = *from
	}
}

// docs:start group-sections

// GroupSections turns a menu's items into its sections. The items must
// already be in the order the menu reads (section, then position, then ID),
// which is the order the repository's index returns them in, so grouping is
// one pass and the sections come out in that same order.
//
// This is the whole reason one table is enough: a section is a heading its
// dishes carry, and it exists exactly as long as a dish carries it.
func GroupSections(items []Item) []Section {
	var sections []Section
	for _, item := range items {
		if n := len(sections); n > 0 && sections[n-1].Name == item.Section {
			sections[n-1].Items = append(sections[n-1].Items, item)
			continue
		}
		sections = append(sections, Section{Name: item.Section, Items: []Item{item}})
	}
	return sections
}

// docs:end group-sections

// clean trims the text fields, normalises the currency and canonicalises the
// dietary flags. It returns a *ValidationError naming the unknown flags,
// with the rest of the fields still cleaned so validate can report its own
// complaints in the same answer.
func (f ItemFields) clean() (ItemFields, error) {
	f.Section = strings.TrimSpace(f.Section)
	f.Name = strings.TrimSpace(f.Name)
	f.Description = strings.TrimSpace(f.Description)
	f.PhotoImageID = strings.TrimSpace(f.PhotoImageID)
	f.Currency = strings.ToUpper(strings.TrimSpace(f.Currency))
	if f.Currency == "" {
		f.Currency = DefaultCurrency
	}
	clean, unknown := CleanDietary(f.Dietary)
	f.Dietary = clean
	if len(unknown) > 0 {
		return f, &ValidationError{Errors: []FieldError{{
			Field:   "dietary",
			Message: fmt.Sprintf("has unknown flags %s: use %s", strings.Join(unknown, ", "), strings.Join(DietaryFlags, ", ")),
		}}}
	}
	return f, nil
}

func (i Item) changedFields(next Item) []string {
	changed := []string{}
	for _, f := range []struct {
		name string
		same bool
	}{
		{"section", i.Section == next.Section},
		{"position", i.Position == next.Position},
		{"name", i.Name == next.Name},
		{"description", i.Description == next.Description},
		{"price_minor", i.PriceMinor == next.PriceMinor},
		{"currency", i.Currency == next.Currency},
		{"available", i.Available == next.Available},
		{"stock", sameStock(i.Stock, next.Stock)},
		{"dietary", slices.Equal(i.Dietary, next.Dietary)},
		{"photo_image_id", i.PhotoImageID == next.PhotoImageID},
	} {
		if !f.same {
			changed = append(changed, f.name)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return changed
}

// sameStock compares two stocks, where nil is unlimited and so equal only to
// another nil.
func sameStock(a, b *int) bool {
	switch {
	case a == nil || b == nil:
		return a == nil && b == nil
	default:
		return *a == *b
	}
}

func (i Item) validate() error {
	var errs []FieldError
	if msg := checkText(i.Section, 1, MaxSectionLength); msg != "" {
		errs = append(errs, FieldError{Field: "section", Message: msg})
	}
	if msg := checkText(i.Name, 1, MaxNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "name", Message: msg})
	}
	if msg := checkText(i.Description, 0, MaxDescriptionLength); msg != "" {
		errs = append(errs, FieldError{Field: "description", Message: msg})
	}
	if i.Position < 0 {
		errs = append(errs, FieldError{Field: "position", Message: "must be 0 or more"})
	}
	// A negative price would mean the restaurant pays the customer to eat.
	if i.PriceMinor < 0 {
		errs = append(errs, FieldError{Field: "price_minor", Message: "must be 0 or more minor units, such as 950 for £9.50"})
	}
	if utf8.RuneCountInString(i.Currency) != CurrencyLength {
		errs = append(errs, FieldError{Field: "currency", Message: fmt.Sprintf("must be a %d-letter ISO 4217 code, such as GBP", CurrencyLength)})
	}
	if i.Stock != nil && *i.Stock < 0 {
		errs = append(errs, FieldError{Field: "stock", Message: "must be 0 or more, or absent for unlimited"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// checkText returns why s isn't a valid text of min to max characters, or "".
func checkText(s string, minLen, maxLen int) string {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return "must be valid text"
	case n < minLen:
		return "is required"
	case n > maxLen:
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	return ""
}
