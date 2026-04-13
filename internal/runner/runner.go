package runner

import (
	"context"

	"github.com/ihavespoons/braid/internal/config"
)

// Invocation describes a single agent call. Adding new options here is
// preferable to growing the RunAgent argument list.
type Invocation struct {
	Agent          config.AgentName
	Model          string
	PermissionMode string // claude --permission-mode value; empty means runner default
	Prompt         string
	OnLine         func(string)
}

// AgentRunner spawns AI agents and streams their output. Implementations
// may be native (subprocess) or docker-sandboxed.
type AgentRunner interface {
	// RunAgent invokes the agent described by inv, emitting each complete
	// stdout line to inv.OnLine as it arrives. Returns the agent's reply
	// on success, or an error if the agent fails.
	RunAgent(ctx context.Context, inv Invocation) (string, error)

	// Stop terminates any in-flight agent invocation (SIGTERM with grace
	// period, then SIGKILL for native runners). Safe to call multiple times.
	Stop() error
}

// Factory creates a new AgentRunner on demand for a given sandbox mode.
// Implementations are provided by higher-level code so this package does
// not depend on the sandbox package.
type Factory func(mode config.SandboxMode) (AgentRunner, error)
