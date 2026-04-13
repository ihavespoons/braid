package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ihavespoons/braid/internal/config"
)

var initForce bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a .braid/ directory with default configuration",
	Long: `Creates .braid/config.json, .braid/docker.json, and .braid/.gitignore in the
current directory. Existing files are preserved unless --force is passed.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite existing config files")
}

func runInit(cmd *cobra.Command, args []string) error {
	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	braidDir := filepath.Join(projectRoot, ".braid")
	if err := os.MkdirAll(braidDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", braidDir, err)
	}

	files := []struct {
		name    string
		content string
	}{
		{"config.json", config.DefaultConfigJSON},
		{"docker.json", config.DefaultDockerJSON},
		{".gitignore", config.DefaultGitignore},
	}

	created := 0
	skipped := 0
	for _, f := range files {
		path := filepath.Join(braidDir, f.name)
		if !initForce {
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("  skip  %s (exists)\n", path)
				skipped++
				continue
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Printf("  wrote %s\n", path)
		created++
	}

	fmt.Printf("\ninit complete: %d created, %d skipped\n", created, skipped)
	return nil
}
