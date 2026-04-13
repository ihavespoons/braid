package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ihavespoons/braid/internal/config"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/sandbox"
)

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild the braid sandbox Docker image",
	Long: `Removes the existing braid-sandbox image (if any) and rebuilds it from
the embedded Dockerfile. Run this after upgrading braid or when the base
image needs a refresh (e.g., new Claude Code CLI version).`,
	RunE: runRebuild,
}

func init() {
	rootCmd.AddCommand(rebuildCmd)
}

func runRebuild(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	braidlog.Phase("Rebuilding " + sandbox.ImageName)

	if err := sandbox.RemoveImage(ctx); err != nil {
		braidlog.Warn("remove existing image: %v (continuing)", err)
	} else {
		braidlog.OK("removed existing image (if any)")
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	dockerCfg, dockerWarn := config.LoadDocker(projectRoot)
	if dockerWarn != nil {
		braidlog.Warn("%v", dockerWarn)
	}

	if err := sandbox.BuildImage(ctx, projectRoot, dockerCfg); err != nil {
		return err
	}
	braidlog.OK("image rebuilt: %s", sandbox.ImageName)
	return nil
}
