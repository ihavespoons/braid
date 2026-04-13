package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/gitutil"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/sandbox"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify braid's dependencies and auth are set up correctly",
	Long: `Checks:
  - git is available
  - claude CLI is available (for native agent mode)
  - docker is available and the daemon is reachable (for sandbox mode)
  - braid sandbox image exists or can be built
  - CLAUDE_CODE_OAUTH_TOKEN is set (or ~/.claude.json exists)
  - .braid/config.json is parseable`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	braidlog.Phase("Braid Doctor")

	failed := 0
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			braidlog.Error("%s — %v", name, err)
			failed++
		} else {
			braidlog.OK("%s", name)
		}
	}

	check("git available", func() error {
		if !gitutil.HasCommandOnPath("git") {
			return fmt.Errorf("git not found on PATH")
		}
		return nil
	})

	check("claude CLI available (native mode)", func() error {
		if !gitutil.HasCommandOnPath("claude") {
			return fmt.Errorf("claude not found on PATH — install from https://claude.com/claude-code")
		}
		// Capture the version to give users a concrete datapoint.
		vcmd := exec.CommandContext(ctx, "claude", "--version")
		var out bytes.Buffer
		vcmd.Stdout = &out
		if err := vcmd.Run(); err == nil {
			version := strings.TrimSpace(out.String())
			if version != "" {
				braidlog.Info("    claude %s", version)
			}
		}
		return nil
	})

	check("docker available (sandbox mode)", func() error {
		return sandbox.DockerAvailable(ctx)
	})

	check("sandbox image exists or can be built", func() error {
		exists, err := sandbox.ImageExists(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("image %s not built yet — run `braid rebuild`", sandbox.ImageName)
		}
		return nil
	})

	check("Claude auth present", func() error {
		if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
			return nil
		}
		home, err := os.UserHomeDir()
		if err == nil {
			for _, rel := range []string{".claude.json", ".claude/credentials.json"} {
				if _, statErr := os.Stat(home + "/" + rel); statErr == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("no CLAUDE_CODE_OAUTH_TOKEN env var and no ~/.claude.json — run `claude login`")
	})

	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	check("project config parseable", func() error {
		if _, err := config.Load(projectRoot); err != nil {
			return err
		}
		if _, err := config.LoadDocker(projectRoot); err != nil {
			return err
		}
		return nil
	})

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	braidlog.OK("all checks passed")
	return nil
}
