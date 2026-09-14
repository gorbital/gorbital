// Command gen regenerates the Minimal recipe templates from
// examples/minimal. Run it with go generate from cli/internal/recipes.
package main

import (
	"fmt"
	"os"

	"apistock.dev/cli/internal/recipes/generate"
)

func main() {
	if err := generate.Run("../../../examples/minimal", "minimal"); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}
