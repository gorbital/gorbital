package domain

import "time"

// DefaultShelfName names the shelf every new reader starts with.
const DefaultShelfName = "Reading"

// Shelf is one of a reader's named shelves.
type Shelf struct {
	ID        string
	OwnerID   string
	Name      string
	CreatedAt time.Time
}
