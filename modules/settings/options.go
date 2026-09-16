package settings

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"time"
	"unicode/utf8"
)

// An Option configures a setting declaration. An option that doesn't fit the
// setting's kind makes the declaration panic at startup.
type Option interface{ apply(*definition) error }

type optionFunc func(*definition) error

func (f optionFunc) apply(d *definition) error { return f(d) }

// Describe sets the help text shown to operators.
func Describe(text string) Option {
	return optionFunc(func(d *definition) error {
		d.description = text
		return nil
	})
}

// Group sets the group used to organise settings in listings. Default: the
// key's first segment.
func Group(name string) Option {
	return optionFunc(func(d *definition) error {
		d.group = name
		return nil
	})
}

// ReasonRequired makes every change to the setting require a reason, recorded
// in its history. Use it for security-relevant settings.
func ReasonRequired() Option {
	return optionFunc(func(d *definition) error {
		d.reasonRequired = true
		return nil
	})
}

// RestartRequired makes [Setting.Get] return the value loaded at startup.
// Changes are stored immediately and take effect when instances restart.
func RestartRequired() Option {
	return optionFunc(func(d *definition) error {
		d.restartRequired = true
		return nil
	})
}

// Range limits an Int, Float or Duration setting to [lo, hi]. The bounds'
// type must match the setting: Range(0.0, 2.0) for a Float.
func Range[T int | float64 | time.Duration](lo, hi T) Option {
	return optionFunc(func(d *definition) error {
		if _, ok := d.def.(T); !ok {
			return fmt.Errorf("Range(%v, %v) takes %T bounds, which don't match a %s setting", lo, hi, lo, d.kind)
		}
		if lo > hi {
			return fmt.Errorf("Range lower bound %v is above upper bound %v", lo, hi)
		}
		d.validators = append(d.validators, func(v any) error {
			if x := v.(T); x < lo || x > hi {
				return fmt.Errorf("must be between %v and %v", lo, hi)
			}
			return nil
		})
		d.constraints["min"], d.constraints["max"] = constraintValue(lo), constraintValue(hi)
		return nil
	})
}

// OneOf limits a String setting, or each item of a StringList, to values.
func OneOf(values ...string) Option {
	return optionFunc(func(d *definition) error {
		if len(values) == 0 {
			return errors.New("OneOf needs at least one value")
		}
		allowed := slices.Clone(values)
		d.constraints["one_of"] = allowed
		return eachString(d, "OneOf", func(s string) error {
			if !slices.Contains(allowed, s) {
				return fmt.Errorf("must be one of %v", allowed)
			}
			return nil
		})
	})
}

// MaxLen limits a String setting, or each item of a StringList, to n
// characters.
func MaxLen(n int) Option {
	return optionFunc(func(d *definition) error {
		if n <= 0 {
			return fmt.Errorf("MaxLen(%d) must be positive", n)
		}
		d.constraints["max_len"] = n
		return eachString(d, "MaxLen", func(s string) error {
			if utf8.RuneCountInString(s) > n {
				return fmt.Errorf("must be at most %d characters", n)
			}
			return nil
		})
	})
}

// DefaultMaxItems limits a StringList setting declared without [MaxItems],
// so a list is never bounded only by the request body limit.
const DefaultMaxItems = 100

// MaxItems limits a StringList setting to n items. Without it, a StringList
// setting is limited to [DefaultMaxItems].
func MaxItems(n int) Option {
	return optionFunc(func(d *definition) error {
		if d.kind != KindStringList {
			return fmt.Errorf("MaxItems applies to string list settings, not %s", d.kind)
		}
		if n <= 0 {
			return fmt.Errorf("MaxItems(%d) must be positive", n)
		}
		d.constraints["max_items"] = n
		d.validators = append(d.validators, func(v any) error {
			if len(v.([]string)) > n {
				return fmt.Errorf("must have at most %d items", n)
			}
			return nil
		})
		return nil
	})
}

// URL requires a String setting, or each item of a StringList, to be an
// absolute http or https URL.
func URL() Option {
	return optionFunc(func(d *definition) error {
		d.constraints["format"] = "url"
		return eachString(d, "URL", func(s string) error {
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("must be an absolute http or https URL")
			}
			return nil
		})
	})
}

// Email requires a String setting, or each item of a StringList, to be a
// bare email address such as no-reply@example.com.
func Email() Option {
	return optionFunc(func(d *definition) error {
		d.constraints["format"] = "email"
		return eachString(d, "Email", func(s string) error {
			addr, err := mail.ParseAddress(s)
			if err != nil || addr.Address != s {
				return errors.New("must be an email address such as name@example.com")
			}
			return nil
		})
	})
}

// Validate adds a custom check. fn's parameter type must match the setting:
// func(time.Duration) error for a Duration. Error messages are shown to
// operators and must not include the value.
func Validate[T any](fn func(T) error) Option {
	return optionFunc(func(d *definition) error {
		var zero T
		if _, ok := d.def.(T); !ok {
			return fmt.Errorf("Validate takes func(%T) error, which doesn't match a %s setting", zero, d.kind)
		}
		d.validators = append(d.validators, func(v any) error { return fn(v.(T)) })
		return nil
	})
}

func eachString(d *definition, option string, check func(string) error) error {
	switch d.kind {
	case KindString, KindEnum:
		d.validators = append(d.validators, func(v any) error { return check(v.(string)) })
	case KindStringList:
		d.validators = append(d.validators, func(v any) error {
			for i, s := range v.([]string) {
				if err := check(s); err != nil {
					return fmt.Errorf("item %d %w", i+1, err)
				}
			}
			return nil
		})
	default:
		return fmt.Errorf("%s applies to string and string list settings, not %s", option, d.kind)
	}
	return nil
}

func constraintValue(v any) any {
	if d, ok := v.(time.Duration); ok {
		return d.String()
	}
	return v
}
