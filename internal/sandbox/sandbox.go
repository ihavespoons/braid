package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/runner"
)

// DockerRunner implements runner.AgentRunner by spawning agents inside the
// braid sandbox image.
type DockerRunner struct {
	projectRoot string
	env         []string
	docker      config.DockerConfig

	mu      sync.Mutex
	current *exec.Cmd
	aborted bool
}

// NewDockerRunner returns a DockerRunner rooted at projectRoot. envPassthrough
// is the list of NAME or NAME=VALUE entries to forward into the container.
// dc controls network policy.
func NewDockerRunner(projectRoot string, envPassthrough []string, dc config.DockerConfig) *DockerRunner {
	return &DockerRunner{
		projectRoot: projectRoot,
		env:         envPassthrough,
		docker:      dc,
	}
}

// Compile-time check that DockerRunner satisfies the interface.
var _ runner.AgentRunner = (*DockerRunner)(nil)

// RunAgent invokes the given agent inside a sandboxed container, streaming
// stdout lines via onLine.
func (r *DockerRunner) RunAgent(ctx context.Context, agent config.AgentName, model, prompt string, onLine func(string)) (string, error) {
	r.mu.Lock()
	if r.aborted {
		r.mu.Unlock()
		return "", errors.New("runner was stopped (cancelled)")
	}
	r.mu.Unlock()

	if err := EnsureImage(ctx, r.projectRoot, r.docker); err != nil {
		return "", err
	}

	cmdName, cmdArgs, err := buildAgentCommand(agent, model)
	if err != nil {
		return "", err
	}

	opts := RunOptions{
		ProjectRoot: r.projectRoot,
		Command:     cmdName,
		Args:        cmdArgs,
		Env:         envWithPassthrough(r.env),
		Docker:      r.docker,
		Remove:      true,
	}

	dockerArgs := buildDockerArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("docker run: %w", err)
	}

	r.mu.Lock()
	r.current = cmd
	r.mu.Unlock()

	// Stream prompt in, close stdin to signal EOF.
	go func() {
		defer stdin.Close()
		_, _ = io.WriteString(stdin, prompt)
	}()

	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderr)
		close(stderrDone)
	}()

	var fullOutput bytes.Buffer
	lb := &runner.LineBuffer{}
	chunk := make([]byte, 4096)
	for {
		n, readErr := stdout.Read(chunk)
		if n > 0 {
			text := string(chunk[:n])
			fullOutput.WriteString(text)
			for _, line := range lb.Push(text) {
				if onLine != nil {
					onLine(line)
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	for _, line := range lb.Flush() {
		if onLine != nil {
			onLine(line)
		}
	}

	<-stderrDone
	waitErr := cmd.Wait()

	r.mu.Lock()
	r.current = nil
	r.mu.Unlock()

	if waitErr != nil {
		return fullOutput.String(), fmt.Errorf("%s (docker) failed: %w: %s", agent, waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return fullOutput.String(), nil
}

// Stop kills any running container. `docker run` inherits signal
// propagation so SIGTERM to the host docker process terminates the
// container as well.
func (r *DockerRunner) Stop() error {
	r.mu.Lock()
	r.aborted = true
	cmd := r.current
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}

// buildAgentCommand returns the binary + argv for the given agent. The
// sandbox currently only supports Claude Code.
func buildAgentCommand(agent config.AgentName, model string) (string, []string, error) {
	switch agent {
	case config.AgentClaude:
		args := []string{"--permission-mode", "acceptEdits", "-p"}
		if model != "" {
			args = append([]string{"--model", model}, args...)
		}
		return "claude", args, nil
	case config.AgentCodex, config.AgentOpenCode:
		return "", nil, fmt.Errorf("agent %q not yet supported in sandbox (Phase 6: claude only)", agent)
	default:
		return "", nil, fmt.Errorf("unknown agent: %q", agent)
	}
}
