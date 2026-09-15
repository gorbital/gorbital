// Command gen regenerates every preset's recipe templates from its golden
// app. Run it with go generate from cli/internal/recipes.
package main

import (
	"fmt"
	"os"

	"apistock.dev/cli/internal/recipes/generate"
)

// goldenApps maps each golden app to the template directory it generates.
var goldenApps = []struct{ src, dst string }{
	{"../../../examples/minimal", "minimal"},
	{"../../../examples/full-single", "full"},
}

func main() {
	for _, app := range goldenApps {
		if err := generate.Run(app.src, app.dst); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}
