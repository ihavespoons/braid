package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ihavespoons/braid/internal/config"
)

// NativeRunner spawns AI agents as direct subprocesses without any sandbox.
// Suitable for local development; not safe for untrusted code.
type NativeRunner struct {
	projectRoot string
	env         []string

	mu      sync.Mutex
	current *exec.Cmd
	aborted bool
}

// NewNative creates a NativeRunner rooted at projectRoot. envPassthrough is
// the list of environment variable names (or NAME=VALUE pairs) that should
// be forwarded to child processes.
func NewNative(projectRoot string, envPassthrough []string) *NativeRunner {
	return &NativeRunner{
		projectRoot: projectRoot,
		env:         envPassthrough,
	}
}

// RunAgent invokes the agent subprocess, streaming stdout lines via onLine.
// Returns the complete stdout on success. Honors ctx cancellation.
func (r *NativeRunner) RunAgent(ctx context.Context, agent config.AgentName, model, prompt string, onLine func(string)) (string, error) {
	r.mu.Lock()
	if r.aborted {
		r.mu.Unlock()
		return "", errors.New("runner was stopped (cancelled)")
	}
	r.mu.Unlock()

	cmdName, args, err := buildCommand(agent, model)
	if err != nil {
		return "", err
	}

	// Pre-check: fail fast with a helpful message when the agent binary
	// isn't on PATH, rather than letting exec.Start surface a cryptic
	// "file not found" error from inside a retry loop.
	if _, err := exec.LookPath(cmdName); err != nil {
		return "", fmt.Errorf("%s CLI not found on PATH — install it or run `braid doctor` for diagnostics", cmdName)
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = r.projectRoot
	cmd.Env = buildEnv(r.env)

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
		return "", fmt.Errorf("starting %s: %w", cmdName, err)
	}

	r.mu.Lock()
	r.current = cmd
	r.mu.Unlock()

	// Write prompt to stdin then close it to signal EOF.
	go func() {
		defer stdin.Close()
		_, _ = io.WriteString(stdin, prompt)
	}()

	// Drain stderr concurrently.
	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderr)
		close(stderrDone)
	}()

	// Stream stdout line-by-line via LineBuffer.
	var fullOutput bytes.Buffer
	lb := &LineBuffer{}
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
			break // io.EOF or closed pipe
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
		return fullOutput.String(), fmt.Errorf("%s failed: %w: %s", agent, waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return fullOutput.String(), nil
}

// Stop terminates any running child with SIGTERM; if it hasn't exited after
// 5 seconds, sends SIGKILL. Subsequent RunAgent calls will fail with an
// "aborted" error.
func (r *NativeRunner) Stop() error {
	r.mu.Lock()
	r.aborted = true
	cmd := r.current
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited; fall through to timeout path.
	}

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

// buildCommand returns the executable and argv for the given agent.
// Phase 2 supports claude only.
func buildCommand(agent config.AgentName, model string) (string, []string, error) {
	switch agent {
	case config.AgentClaude:
		args := []string{"--permission-mode", "acceptEdits", "-p"}
		if model != "" {
			args = append([]string{"--model", model}, args...)
		}
		return "claude", args, nil
	case config.AgentCodex, config.AgentOpenCode:
		return "", nil, fmt.Errorf("agent %q not yet supported (Phase 2: claude only)", agent)
	default:
		return "", nil, fmt.Errorf("unknown agent: %q", agent)
	}
}

// buildEnv constructs the child process env from the current os environ
// plus any NAME=VALUE or NAME entries listed in passthrough.
func buildEnv(passthrough []string) []string {
	env := os.Environ()
	for _, entry := range passthrough {
		if eq := strings.Index(entry, "="); eq >= 0 {
			env = append(env, entry)
			continue
		}
		if val, ok := os.LookupEnv(entry); ok {
			env = append(env, entry+"="+val)
		}
	}
	return env
}
