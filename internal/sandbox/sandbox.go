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
// stdout lines via inv.OnLine.
func (r *DockerRunner) RunAgent(ctx context.Context, inv runner.Invocation) (string, error) {
	r.mu.Lock()
	if r.aborted {
		r.mu.Unlock()
		return "", errors.New("runner was stopped (cancelled)")
	}
	r.mu.Unlock()

	agent := inv.Agent
	prompt := inv.Prompt
	onLine := inv.OnLine

	if err := EnsureImage(ctx, r.projectRoot, r.docker); err != nil {
		return "", err
	}

	cmdName, cmdArgs, err := buildAgentCommand(agent, inv.Model, inv.PermissionMode)
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

	var stream *runner.ClaudeStream
	if agent == config.AgentClaude {
		stream = runner.NewClaudeStream()
	}
	emit := func(line string) {
		if stream != nil {
			if display, show := stream.Push(line); show && onLine != nil {
				onLine(display)
			}
			return
		}
		if onLine != nil {
			onLine(line)
		}
	}

	var fullOutput bytes.Buffer
	lb := &runner.LineBuffer{}
	chunk := make([]byte, 4096)
	for {
		n, readErr := stdout.Read(chunk)
		if n > 0 {
			text := string(chunk[:n])
			fullOutput.WriteString(text)
			for _, line := range lb.Push(text) {
				emit(line)
			}
		}
		if readErr != nil {
			break
		}
	}
	for _, line := range lb.Flush() {
		emit(line)
	}

	<-stderrDone
	waitErr := cmd.Wait()

	r.mu.Lock()
	r.current = nil
	r.mu.Unlock()

	output := fullOutput.String()
	if stream != nil && stream.Result() != "" {
		output = stream.Result()
	}

	if waitErr != nil {
		return output, fmt.Errorf("%s (docker) failed: %w: %s", agent, waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return output, nil
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
// sandbox currently only supports Claude Code. permissionMode defaults to
// "bypassPermissions" because the docker sandbox already constrains
// network and filesystem access — interactive permission prompts would
// just hang the headless -p invocation.
func buildAgentCommand(agent config.AgentName, model, permissionMode string) (string, []string, error) {
	switch agent {
	case config.AgentClaude:
		mode := permissionMode
		if mode == "" {
			mode = "bypassPermissions"
		}
		args := []string{
			"--permission-mode", mode,
			"-p",
			"--output-format", "stream-json",
			"--verbose",
		}
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
