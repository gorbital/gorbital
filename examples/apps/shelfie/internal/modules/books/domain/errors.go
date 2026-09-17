package domain

import "errors"

// docs:start errors

// Errors of the books use cases. module.go maps each to an HTTP status and
// a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a signed-in reader.
	ErrUnauthenticated = errors.New("books: a signed-in reader is required")
	// ErrBookNotFound reports a book that doesn't exist or is on another
	// reader's shelf; the two aren't told apart, so IDs can't be probed.
	ErrBookNotFound = errors.New("books: book not found")
	// ErrTitleRequired reports a blank title, or one over 300 characters.
	ErrTitleRequired = errors.New("books: title is required, up to 300 characters")
	// ErrAuthorTooLong reports an author over 200 characters.
	ErrAuthorTooLong = errors.New("books: author is too long")
	// ErrInvalidISBN reports an ISBN that isn't an ISBN-10 or ISBN-13.
	ErrInvalidISBN = errors.New("books: invalid ISBN")
	// ErrInvalidStatus reports an unknown status.
	ErrInvalidStatus = errors.New("books: invalid status")
	// ErrISBNTaken reports an ISBN already on the reader's shelf.
	ErrISBNTaken = errors.New("books: this ISBN is already on the shelf")
	// ErrShelfFull reports a shelf holding as many books as the
	// books.shelf_limit runtime setting allows.
	ErrShelfFull = errors.New("books: the shelf is full")
)

// docs:end errors
