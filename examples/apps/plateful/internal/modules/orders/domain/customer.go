package domain

import "strings"

// Field limits of a customer's profile. The migration's CHECK constraints
// match them.
const (
	MaxDisplayNameLength = 80
)

// docs:start customer-profile

// A Customer is who ordered and where they usually want food taken. It
// belongs to a sign-in account and to no organisation: a customer is a
// member of nothing on this platform, which is exactly why the profile is
// keyed by the account rather than by a tenant.
type Customer struct {
	UserID string
	CustomerFields
}

// CustomerFields are what the customer sets, at registration and afterwards.
type CustomerFields struct {
	DisplayName string
	// Address is where they usually want an order taken, "" until they say.
	// An order may always name a different one.
	Address string
}

// Clean trims the fields and returns them, or a *ValidationError. It is the
// rule the registration form and any later edit both go through, so a
// display name of three spaces is refused in the same way wherever it
// arrives.
func (f CustomerFields) Clean() (CustomerFields, error) {
	f.DisplayName = strings.TrimSpace(f.DisplayName)
	f.Address = strings.TrimSpace(f.Address)
	var errs []FieldError
	if msg := checkText(f.DisplayName, 1, MaxDisplayNameLength); msg != "" {
		errs = append(errs, FieldError{Field: "display_name", Message: msg})
	}
	if msg := checkText(f.Address, 0, MaxAddressLength); msg != "" {
		errs = append(errs, FieldError{Field: "address", Message: msg})
	}
	if len(errs) > 0 {
		return f, &ValidationError{Errors: errs}
	}
	return f, nil
}

// docs:end customer-profile
