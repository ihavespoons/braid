package sandbox

import (
	"slices"
	"strings"
	"testing"

	"github.com/ihavespoons/braid/internal/config"
)

func TestBuildDockerArgs_Defaults(t *testing.T) {
	args := buildDockerArgs(RunOptions{
		ProjectRoot: "/home/u/proj",
		Command:     "claude",
		Args:        []string{"-p"},
		Remove:      true,
		Docker:      config.DefaultDockerConfig(),
	})

	// Core structural expectations.
	if args[0] != "run" {
		t.Errorf("first arg should be 'run', got %q", args[0])
	}
	if !slices.Contains(args, "--rm") {
		t.Error("expected --rm")
	}
	if !slices.Contains(args, "-i") {
		t.Error("expected -i")
	}
	if slices.Contains(args, "-t") {
		t.Error("TTY should not be allocated for non-interactive")
	}
	if !slices.Contains(args, "--cap-add") {
		t.Error("expected --cap-add NET_ADMIN")
	}

	// Bind mount present with expected path.
	idx := slices.Index(args, "-v")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatalf("expected -v mount, got %v", args)
	}
	if args[idx+1] != "/home/u/proj:/workspace" {
		t.Errorf("mount: got %q", args[idx+1])
	}

	// Restricted network by default — env var set.
	if !envPresent(args, "BRAID_NETWORK_RESTRICTED=1") {
		t.Error("default config is restricted; expected BRAID_NETWORK_RESTRICTED=1")
	}
	if !envPresentPrefix(args, "BRAID_ALLOWED_HOSTS=") {
		t.Error("expected BRAID_ALLOWED_HOSTS env var")
	}

	// Image and command at the end.
	imgIdx := slices.Index(args, ImageName)
	if imgIdx < 0 {
		t.Fatal("image name not in args")
	}
	if args[imgIdx+1] != "claude" || args[imgIdx+2] != "-p" {
		t.Errorf("command tail: got %v", args[imgIdx+1:])
	}
}

func TestBuildDockerArgs_Interactive(t *testing.T) {
	args := buildDockerArgs(RunOptions{
		ProjectRoot: "/p",
		Interactive: true,
		Remove:      true,
		Docker:      config.DefaultDockerConfig(),
	})
	if !slices.Contains(args, "-t") {
		t.Error("interactive mode should allocate TTY (-t)")
	}
}

func TestBuildDockerArgs_UnrestrictedSkipsEnv(t *testing.T) {
	dc := config.DockerConfig{Network: config.DockerNetwork{
		Mode: config.DockerNetworkUnrestricted,
	}}
	args := buildDockerArgs(RunOptions{
		ProjectRoot: "/p",
		Docker:      dc,
		Remove:      true,
	})
	if envPresent(args, "BRAID_NETWORK_RESTRICTED=1") {
		t.Error("unrestricted mode should not set BRAID_NETWORK_RESTRICTED")
	}
}

func TestHostsFromDocker_AlwaysIncludesAnthropic(t *testing.T) {
	dc := config.DefaultDockerConfig()
	got := HostsFromDocker(dc)
	if !strings.Contains(got, "api.anthropic.com") {
		t.Errorf("expected Anthropic API in allowlist, got %q", got)
	}
}

func TestHostsFromDocker_Dedups(t *testing.T) {
	dc := config.DockerConfig{Network: config.DockerNetwork{
		AllowedHosts: []string{"api.anthropic.com", "api.anthropic.com", "example.com"},
	}}
	got := HostsFromDocker(dc)
	// Should contain each host exactly once.
	if strings.Count(got, "api.anthropic.com") != 1 {
		t.Errorf("expected anthropic once, got %q", got)
	}
	if !strings.Contains(got, "example.com") {
		t.Errorf("missing example.com, got %q", got)
	}
}

// envPresent reports whether args contains a "-e VALUE" pair with the
// given value.
func envPresent(args []string, want string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-e" && args[i+1] == want {
			return true
		}
	}
	return false
}

// envPresentPrefix is like envPresent but matches any value starting with
// prefix (for env vars with dynamic values).
func envPresentPrefix(args []string, prefix string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-e" && strings.HasPrefix(args[i+1], prefix) {
			return true
		}
	}
	return false
}
