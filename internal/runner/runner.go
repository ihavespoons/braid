package runner

import (
	"context"

	"github.com/ihavespoons/braid/internal/config"
)

// AgentRunner spawns AI agents and streams their output. Implementations
// may be native (subprocess) or docker-sandboxed.
type AgentRunner interface {
	// RunAgent invokes agent with model and prompt, emitting each complete
	// stdout line to onLine as it arrives. Returns the full concatenated
	// stdout on success, or an error if the agent fails.
	RunAgent(ctx context.Context, agent config.AgentName, model, prompt string, onLine func(string)) (string, error)

	// Stop terminates any in-flight agent invocation (SIGTERM with grace
	// period, then SIGKILL for native runners). Safe to call multiple times.
	Stop() error
}

// Factory creates a new AgentRunner on demand for a given sandbox mode.
// Implementations are provided by higher-level code so this package does
// not depend on the sandbox package.
type Factory func(mode config.SandboxMode) (AgentRunner, error)
