package widget_test

import (
	"fmt"

	"gorbital.dev/widget"
)

func ExampleNew() {
	w := widget.New("a")
	fmt.Println(w.Name)
	// Output: a
}

// A widget spins.
func ExampleWidget_Spin_twice() {
	w := widget.New("b")
	w.Spin()
	w.Spin()
}
