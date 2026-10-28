// Command fancyband-tui is the Limoni terminal client for the Fancyband engine.
//
// The client is built in Phase 3 of docs/scenario-2-plan.md. Until then this
// entry point only reports its version.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is the client version. Release builds override it with
// -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("fancyband-tui", version)
		return
	}

	fmt.Fprintln(os.Stderr, "fancyband-tui: the client is not built yet; see docs/scenario-2-plan.md, Phase 3")
	os.Exit(2)
}
