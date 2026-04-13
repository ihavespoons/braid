package cmd

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ihavespoons/braid/internal/config"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/sandbox"
)

var shellUnrestricted bool

var shellCmd = &cobra.Command{
	Use:   "shell [-- command args...]",
	Short: "Open an interactive shell inside the braid sandbox",
	Long: `Starts a container from the braid-sandbox image with the current
directory bind-mounted at /workspace. Useful for debugging agent setup
or running ad-hoc commands inside the sandbox.

With no arguments: starts an interactive bash shell.
With a command: runs that command and exits.

Use --unrestricted to disable the network firewall (grants full outbound
access — useful for installing packages before a braid run).`,
	// Accept arbitrary tail args so users can run "braid shell -- ls /workspace".
	DisableFlagsInUseLine: true,
	RunE:                  runShell,
}

func init() {
	shellCmd.Flags().BoolVar(&shellUnrestricted, "unrestricted", false, "disable network firewall")
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}

	dockerCfg, dockerWarn := config.LoadDocker(projectRoot)
	if dockerWarn != nil {
		braidlog.Warn("%v", dockerWarn)
	}
	if shellUnrestricted {
		dockerCfg.Network.Mode = config.DockerNetworkUnrestricted
	}

	if err := sandbox.EnsureImage(ctx, projectRoot, dockerCfg); err != nil {
		return err
	}

	// Build run args. Interactive when no trailing command, non-interactive
	// shell still gets stdin but not TTY when a command is provided.
	interactive := len(args) == 0
	runOpts := sandbox.RunOptions{
		ProjectRoot: projectRoot,
		Docker:      dockerCfg,
		Remove:      true,
		Interactive: interactive,
	}
	if len(args) > 0 {
		runOpts.Command = args[0]
		runOpts.Args = args[1:]
	}

	dockerArgs := sandbox.BuildDockerArgsForShell(runOpts)
	dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	if err := dockerCmd.Run(); err != nil {
		return err
	}
	return nil
}
