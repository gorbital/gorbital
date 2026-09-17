// Command gen regenerates every preset's recipe templates from its golden
// app. Run it with go generate from cli/internal/recipes.
package main

import (
	"context"
	"fmt"
	"os"

	"gorbital.dev/cli/internal/recipes/generate"
)

// goldenApps maps each golden app to the template directory it generates.
var goldenApps = []struct{ src, dst string }{
	{"../../../examples/minimal", "minimal"},
	{"../../../examples/v0.1/full-single", "full"},
	{"../../../examples/v0.1/full-multi", "full-multi"},
}

func main() {
	for _, app := range goldenApps {
		// Only files git tracks or would track become templates: anything
		// git-ignored next to a golden app (.env, keys, coverage output) stays
		// on this machine. Outside a git work tree, such as a source archive,
		// generate.Skipped still leaves local environment files out.
		files, ok, err := generate.GitFiles(context.Background(), app.src)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
		var opts []generate.Option
		if ok {
			opts = append(opts, generate.OnlyFiles(files))
		} else {
			fmt.Fprintf(os.Stderr, "gen: %s isn't in a git work tree; git-ignored files other than .env files would become templates\n", app.src)
		}
		if err := generate.Run(app.src, app.dst, opts...); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}
