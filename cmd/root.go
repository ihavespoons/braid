// Package cmd wires braid's CLI surface via cobra.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is the braid build version. Set via -ldflags at build time.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "braid",
	Short: "Braid orchestrates AI code agents through composable operators",
	Long: `Braid coordinates AI code agents (Claude Code) through a composable
operator language: review loops, parallel races, A/B comparisons, and
multi-task progression.

OPERATORS
  review              work → review → gate loop (default 3 iterations)
  xN / repeat N       run the inner pipeline N times sequentially
  ralph [N] "gate"    iterate tasks until the gate emits DONE (max N)
  vN / race N         run N parallel implementations, resolver picks best
  vs                  fork into two branches (A vs B)
  pick "criteria"     resolver: judge agent selects the winning branch
  merge "criteria"    resolver: synthesize branches into one
  compare             resolver: generate a markdown comparison doc

EXAMPLES
  braid "fix the bug"
  braid "fix the bug" review
  braid "fix the bug" review x3
  braid "approach A" vs "approach B" pick "most robust"
  braid "task" v3 pick "best implementation"
  braid "task" ralph 5 "done when all tasks complete"
  braid --agent claude --sandbox docker "task" review

Run 'braid init' to scaffold a .braid/ config dir, 'braid doctor' to
verify your setup, and 'braid shell' to poke around the sandbox.
`,
	Version:      Version,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
}
