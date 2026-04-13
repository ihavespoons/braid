package main

import (
	"os"

	"github.com/ihavespoons/braid/cmd"
)

// knownSubcommands are handled by cobra. Everything else flows into the
// default run pipeline, which expects a free-form positional expression that
// may contain reserved keywords (review, vs, pick, ...).
var knownSubcommands = map[string]bool{
	"init":       true,
	"doctor":     true,
	"shell":      true,
	"rebuild":    true,
	"help":       true,
	"completion": true,
}

func main() {
	if len(os.Args) > 1 {
		first := os.Args[1]
		// Flags and help flags route through cobra regardless.
		if !knownSubcommands[first] && first != "-h" && first != "--help" && first != "-v" && first != "--version" {
			// Skip cobra entirely for the default run command.
			cmd.RunDefaultArgs(os.Args[1:])
			return
		}
	}
	cmd.Execute()
}

