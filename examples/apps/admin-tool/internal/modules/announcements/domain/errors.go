package domain

import "errors"

// docs:start errors

// Errors of the announcements use cases. module.go maps each to an HTTP
// status and a problem code, which are public API.
var (
	// ErrTitleRequired reports a blank title, or one over MaxTitle
	// characters.
	ErrTitleRequired = errors.New("announcements: title is required, up to 200 characters")
	// ErrBodyRequired reports a blank body, or one over MaxBody characters.
	ErrBodyRequired = errors.New("announcements: body is required, up to 5000 characters")
	// ErrInvalidEndsAt reports an end time that isn't in the future.
	ErrInvalidEndsAt = errors.New("announcements: ends_at must be in the future")
	// ErrTooManyAnnouncements reports as many active announcements as the
	// announcements.max_active runtime setting allows.
	ErrTooManyAnnouncements = errors.New("announcements: too many active announcements")
	// ErrAnnouncementNotFound reports a withdrawal of an announcement that
	// isn't there, whether it never was or someone withdrew it first.
	ErrAnnouncementNotFound = errors.New("announcements: no announcement with this ID")
	// ErrReasonRequired reports a withdrawal without a reason to record.
	ErrReasonRequired = errors.New("announcements: a withdrawal needs a reason")
)

// docs:end errors
