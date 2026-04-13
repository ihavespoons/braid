package sandbox

import (
	"os"
	"strconv"

	"github.com/ihavespoons/braid/internal/config"
)

// RunOptions describes how to invoke the container: what to mount, which
// env vars to pass, whether the network is restricted, and whether the
// command is interactive (TTY).
type RunOptions struct {
	// ProjectRoot is bind-mounted at /workspace inside the container.
	ProjectRoot string

	// Command + Args run inside the container after the entrypoint.
	Command string
	Args    []string

	// Env is a list of NAME=VALUE strings to pass through to the container.
	Env []string

	// Docker network config from .braid/docker.json.
	Docker config.DockerConfig

	// Interactive requests a TTY (for `braid shell`).
	Interactive bool

	// Remove the container on exit (always true for ephemeral runs).
	Remove bool

	// Name for the container (auto-generated if empty). Visible in docker ps.
	Name string
}

// BuildDockerArgsForShell is the exported wrapper used by `braid shell`.
// Kept separate from the unexported builder so tests and the sandbox runner
// can use the private form while cmd/ can call the public one.
func BuildDockerArgsForShell(opts RunOptions) []string {
	return buildDockerArgs(opts)
}

// buildDockerArgs assembles the full argv for `docker run ...` based on opts.
func buildDockerArgs(opts RunOptions) []string {
	args := []string{"run"}
	if opts.Remove {
		args = append(args, "--rm")
	}
	// stdin/stdout plumbing. Always pipe stdin; only allocate a TTY for
	// interactive shells (it would break stdout line-by-line streaming).
	args = append(args, "-i")
	if opts.Interactive {
		args = append(args, "-t")
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	// CAP_NET_ADMIN is required for iptables inside the entrypoint to
	// apply network restrictions. Granted unconditionally — if the
	// user doesn't want restriction, BRAID_NETWORK_RESTRICTED=0 makes
	// entrypoint skip the rules; the cap is harmless without use.
	args = append(args, "--cap-add", "NET_ADMIN")

	// Bind-mount the project root at /workspace. The entrypoint cds there
	// before exec because WORKDIR is /workspace in the Dockerfile.
	args = append(args, "-v", opts.ProjectRoot+":/workspace")

	// Match host UID/GID so files the agent creates are owned by the user.
	args = append(args, "-e", "BRAID_UID="+strconv.Itoa(os.Getuid()))
	args = append(args, "-e", "BRAID_GID="+strconv.Itoa(os.Getgid()))

	// Network mode. restricted → iptables in entrypoint; unrestricted →
	// default bridge, no egress filtering.
	if opts.Docker.Network.Mode == config.DockerNetworkRestricted {
		args = append(args, "-e", "BRAID_NETWORK_RESTRICTED=1")
		if hosts := HostsFromDocker(opts.Docker); hosts != "" {
			args = append(args, "-e", "BRAID_ALLOWED_HOSTS="+hosts)
		}
	}

	// Pass through user-specified env vars.
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}

	args = append(args, ImageName)

	// Command + args. If Command is empty, the Dockerfile's CMD (bash) runs.
	if opts.Command != "" {
		args = append(args, opts.Command)
		args = append(args, opts.Args...)
	}

	return args
}

// envWithPassthrough converts a list of NAME or NAME=VALUE entries into
// NAME=VALUE pairs ready to be passed to docker -e. Bare NAME entries pick
// up the current process's value.
func envWithPassthrough(entries []string) []string {
	out := []string{}
	for _, e := range entries {
		if e == "" {
			continue
		}
		if idx := indexByte(e, '='); idx >= 0 {
			out = append(out, e)
			continue
		}
		if v, ok := os.LookupEnv(e); ok {
			out = append(out, e+"="+v)
		}
	}
	return out
}

// indexByte is a trivial strings.IndexByte replacement that keeps this
// file free of extra imports.
func indexByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}
