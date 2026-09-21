package records

import (
	"context"
	"errors"
	"testing"

	"gorbital.dev/gorbital"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/repository"
)

// TestPolicyIsImplemented fails until policy.go decides who may see and
// change a record. That is deliberate, and it is the whole point of
// --scope custom: the module is generated red, because gorbital cannot
// know the rule and a module that serves every row to everyone must not
// look finished.
//
// Delete nothing here. Write the three methods, and this test goes green
// on its own. Then add the tests this one cannot write: for each kind of
// caller your rule names, one that may and one that may not.
func TestPolicyIsImplemented(t *testing.T) {
	ctx := context.Background()
	p := Policy{}
	record := domain.Record{ID: "rcr_example", CreatedBy: "usr_example"}

	for _, tt := range []struct {
		method string
		err    error
	}{
		{"CanRead", p.CanRead(ctx, record)},
		{"CanWrite", p.CanWrite(ctx, record)},
		{"Filter", p.Filter(ctx, &repository.Query{})},
	} {
		if errors.Is(tt.err, gorbital.ErrNotImplemented) {
			t.Errorf("Policy.%s isn't written yet: decide who may see and change a record in policy.go, and delete the gorbital.ErrNotImplemented it returns", tt.method)
		}
	}
}

// TestFilterNarrowsTheList fails while Filter adds no condition. A list
// query nothing narrows returns every record in the table to whoever
// asked, which is what a record with a custom rule is not.
//
// Replace this with the real cases once Filter is written: a caller who
// sees some rows, and one who sees none.
func TestFilterNarrowsTheList(t *testing.T) {
	q := &repository.Query{}
	if err := (Policy{}).Filter(context.Background(), q); err != nil {
		return // TestPolicyIsImplemented reports it
	}
	if conditions, _ := q.Conditions(); len(conditions) == 0 {
		t.Error("Policy.Filter added no condition, so GET /v1/records returns every record in the table to every caller with records.record.read")
	}
}
