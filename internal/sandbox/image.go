// Package sandbox runs braid agents inside Docker containers for isolation.
// It shells out to the `docker` CLI rather than using the Docker SDK — this
// keeps dependencies lean and lets users substitute podman via alias.
package sandbox

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ihavespoons/braid/internal/config"
)

// ImageName is the fixed tag braid uses for its sandbox image.
const ImageName = "braid-sandbox:latest"

//go:embed assets/Dockerfile
var dockerfileContent []byte

//go:embed assets/entrypoint.sh
var entrypointContent []byte

// DockerAvailable returns nil if the `docker` binary is on PATH and its
// daemon responds to `docker version`.
func DockerAvailable(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("docker not found on PATH")
	}
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("docker daemon not reachable: %w", err)
		}
		return fmt.Errorf("docker daemon not reachable: %s", msg)
	}
	return nil
}

// ImageExists reports whether the braid image has already been built.
func ImageExists(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", ImageName, "--format", "{{.Id}}")
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// docker image inspect returns exit 1 when the image doesn't exist.
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil
	}
	return false, err
}

// EnsureImage builds the sandbox image if it doesn't already exist.
// Returns a friendly error if docker is unreachable. projectRoot is used
// to resolve a relative dc.Dockerfile path; either may be zero-valued to
// use the embedded Dockerfile.
func EnsureImage(ctx context.Context, projectRoot string, dc config.DockerConfig) error {
	if err := DockerAvailable(ctx); err != nil {
		return err
	}
	exists, err := ImageExists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return BuildImage(ctx, projectRoot, dc)
}

// BuildImage forces a rebuild of the sandbox image. Used by `braid rebuild`.
// Writes Dockerfile + entrypoint.sh to a temp directory and invokes
// `docker build`, streaming output to the caller's stderr. If
// dc.Dockerfile is set, it is read (relative to projectRoot when not
// absolute) and used in place of the embedded Dockerfile.
func BuildImage(ctx context.Context, projectRoot string, dc config.DockerConfig) error {
	if err := DockerAvailable(ctx); err != nil {
		return err
	}

	dockerfile, source, err := resolveDockerfile(projectRoot, dc)
	if err != nil {
		return err
	}

	buildCtx, err := os.MkdirTemp("", "braid-build-*")
	if err != nil {
		return fmt.Errorf("creating build context: %w", err)
	}
	defer os.RemoveAll(buildCtx)

	if err := os.WriteFile(filepath.Join(buildCtx, "Dockerfile"), dockerfile, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "entrypoint.sh"), entrypointContent, 0o755); err != nil {
		return err
	}

	if source != "" {
		fmt.Fprintf(os.Stderr, "[braid] building sandbox image from custom Dockerfile: %s\n", source)
	}

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", ImageName, buildCtx)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}

// resolveDockerfile returns the Dockerfile bytes and, when a custom one
// is used, the resolved path (for logging). An empty source means the
// embedded default was used.
func resolveDockerfile(projectRoot string, dc config.DockerConfig) ([]byte, string, error) {
	if dc.Dockerfile == "" {
		return dockerfileContent, "", nil
	}
	path := dc.Dockerfile
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectRoot, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading custom Dockerfile %s: %w", path, err)
	}
	return data, path, nil
}

// RemoveImage deletes the sandbox image if present.
func RemoveImage(ctx context.Context) error {
	exists, err := ImageExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", ImageName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rmi: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// HostsFromDocker produces the comma-separated allowlist that the
// entrypoint script expects via BRAID_ALLOWED_HOSTS. Adds the default
// Anthropic API host when Claude is the active agent.
func HostsFromDocker(dc config.DockerConfig) string {
	seen := map[string]bool{}
	hosts := []string{}
	add := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	for _, h := range dc.Network.AllowedHosts {
		add(h)
	}
	// Always include Anthropic's API so Claude can function.
	add("api.anthropic.com")
	return strings.Join(hosts, ",")
}
