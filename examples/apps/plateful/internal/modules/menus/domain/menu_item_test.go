package domain_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"example.com/plateful/internal/modules/menus/domain"
)

var now = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// fields is a valid dish to start from.
func fields() domain.ItemFields {
	return domain.ItemFields{Section: "Mains", Name: "Margherita", PriceMinor: 1050, Available: true}
}

func TestNewItemCleansItsFields(t *testing.T) {
	f := fields()
	f.Section, f.Name = "  Mains  ", "  Margherita  "
	f.Currency = ""
	f.Dietary = []string{"VEGAN", " vegetarian", "vegan", ""}

	item, err := domain.NewItem("mnu_1", "org_1", "usr_1", f, now)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	if item.Section != "Mains" || item.Name != "Margherita" {
		t.Errorf("item = %+v, want the section and name trimmed", item)
	}
	if item.Currency != domain.DefaultCurrency {
		t.Errorf("currency = %q, want %q when the request says nothing", item.Currency, domain.DefaultCurrency)
	}
	if want := []string{"vegan", "vegetarian"}; !slices.Equal(item.Dietary, want) {
		t.Errorf("dietary = %v, want %v: lowercased, deduplicated and sorted so the stored value is canonical", item.Dietary, want)
	}
	if item.Stock != nil {
		t.Errorf("stock = %v, want nil: a dish is unlimited until someone counts it", item.Stock)
	}
}

func TestNewItemRejectsInvalidFields(t *testing.T) {
	negative := -1
	for _, tt := range []struct {
		name  string
		field string
		set   func(*domain.ItemFields)
	}{
		{"a blank section", "section", func(f *domain.ItemFields) { f.Section = "  " }},
		{"a blank name", "name", func(f *domain.ItemFields) { f.Name = "" }},
		{"a negative price", "price_minor", func(f *domain.ItemFields) { f.PriceMinor = -1 }},
		{"a two-letter currency", "currency", func(f *domain.ItemFields) { f.Currency = "GB" }},
		{"negative stock", "stock", func(f *domain.ItemFields) { f.Stock = &negative }},
		{"a negative position", "position", func(f *domain.ItemFields) { f.Position = -1 }},
		{"an unknown dietary flag", "dietary", func(f *domain.ItemFields) { f.Dietary = []string{"pescatarian"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := fields()
			tt.set(&f)
			_, err := domain.NewItem("mnu_1", "org_1", "usr_1", f, now)
			var invalid *domain.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("NewItem = %v, want a *ValidationError", err)
			}
			if !errors.Is(err, domain.ErrInvalidItem) {
				t.Errorf("NewItem error doesn't unwrap to ErrInvalidItem, so module.go can't map it")
			}
			if invalid.Errors[0].Field != tt.field {
				t.Errorf("field = %q, want %q", invalid.Errors[0].Field, tt.field)
			}
		})
	}
}

// TestApplyReportsWhatChanged checks that an update that changes nothing is
// not a change, which is what keeps the version from moving for free.
func TestApplyReportsWhatChanged(t *testing.T) {
	item, err := domain.NewItem("mnu_1", "org_1", "usr_1", fields(), now)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	later := now.Add(time.Hour)

	same := item.Section
	if _, changed, err := item.Apply(domain.Changes{Section: &same}, later); err != nil || changed != nil {
		t.Errorf("Apply with the same section = %v, %v; want no change", changed, err)
	}

	price := int64(1150)
	stock := 12
	next, changed, err := item.Apply(domain.Changes{PriceMinor: &price, SetStock: true, Stock: &stock}, later)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"price_minor", "stock"}; !slices.Equal(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}
	if next.PriceMinor != 1150 || !next.UpdatedAt.Equal(later) {
		t.Errorf("item = %+v, want the new price and time", next)
	}

	// SetStock with no stock is how a kitchen stops counting portions.
	unlimited, changed, err := next.Apply(domain.Changes{SetStock: true}, later)
	if err != nil || !slices.Equal(changed, []string{"stock"}) || unlimited.Stock != nil {
		t.Errorf("Apply clearing the stock = %+v, %v, %v; want unlimited stock", unlimited.Stock, changed, err)
	}
}

// TestGroupSections is the reason one table is enough: a section is a
// heading its dishes carry, and grouping rows already in menu order is all
// it takes to read one back out.
func TestGroupSections(t *testing.T) {
	item := func(section, name string, position int) domain.Item {
		return domain.Item{ItemFields: domain.ItemFields{Section: section, Name: name, Position: position}}
	}
	sections := domain.GroupSections([]domain.Item{
		item("Mains", "Calzone", 0),
		item("Mains", "Margherita", 1),
		item("Puddings", "Tiramisu", 0),
	})
	if len(sections) != 2 {
		t.Fatalf("sections = %+v, want two", sections)
	}
	if sections[0].Name != "Mains" || len(sections[0].Items) != 2 || sections[0].Items[0].Name != "Calzone" {
		t.Errorf("first section = %+v, want the two mains in their order", sections[0])
	}
	if sections[1].Name != "Puddings" || len(sections[1].Items) != 1 {
		t.Errorf("second section = %+v, want the one pudding", sections[1])
	}
	if got := domain.GroupSections(nil); got != nil {
		t.Errorf("GroupSections(nil) = %+v, want no sections: a menu with no dishes has no headings", got)
	}
}
