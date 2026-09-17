// Package gadget makes gadgets.
package gadget

// Make makes a gadget. It is new and has an example.
func Make() {}

func Undocumented() {}

// Fresh is new and has no example.
func Fresh() {}

// Old was released and has no example.
func Old() {}

// Gadget is a gadget.
type Gadget struct{}

func (Gadget) Run() {}

const (
	Loose = 1
	// Tight is documented.
	Tight = 2
)
