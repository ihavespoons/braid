// Package executor walks braid's AST, dispatching each node to a handler
// that invokes agents, threads context between steps, and manages cleanup.
package executor

import (
	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/tui"
)

// Verdict is the terminal status of a review loop.
type Verdict string

const (
	VerdictNone          Verdict = ""
	VerdictDone          Verdict = "DONE"
	VerdictIterate       Verdict = "ITERATE"
	VerdictMaxIterations Verdict = "MAX_ITERATIONS"
)

// ExecutionContext is threaded through the recursive walker. It carries the
// per-run config plus loop counters that templates may interpolate.
type ExecutionContext struct {
	ProjectRoot string
	Config      *config.BraidConfig
	Flags       *ast.ParsedFlags
	StepConfig  map[config.StepName]config.StepSelection
	BraidMD     string
	ShowRequest bool

	// Threading state — updated when descending into inner nodes.
	LastMessage     string
	RepeatPass      int
	MaxRepeatPasses int
	RalphIteration  int
	MaxRalph        int

	// Events, if non-nil, receives typed events for TUI rendering.
	// Handlers should use tui.Emitter(ec.Events).Send(...) for nil safety.
	Events chan<- tui.Event
}

// emit is a convenience that forwards to tui.Emitter, letting handlers
// write ec.emit(event) without threading the channel everywhere.
func (ec *ExecutionContext) emit(ev tui.Event) {
	tui.Emitter(ec.Events).Send(ev)
}

// ExecutionResult summarizes the outcome of executing one node.
type ExecutionResult struct {
	LastMessage string
	LogFile     string
	Verdict     Verdict
	Iterations  int
}

// ResolveStepConfig builds the fully-resolved per-step StepSelection map
// using braid's precedence chain:
//
//	per-step flag > per-step config > global flag > global config > default
//
// Sandbox has no per-step flag.
func ResolveStepConfig(cfg *config.BraidConfig, flags *ast.ParsedFlags) map[config.StepName]config.StepSelection {
	defaults := struct {
		agent   config.AgentName
		model   string
		sandbox config.SandboxMode
	}{
		agent:   firstAgent(config.AgentName(flags.Agent), cfg.Agent, config.AgentClaude),
		model:   firstString(flags.Model, cfg.Model),
		sandbox: firstSandbox(config.SandboxMode(flags.Sandbox), cfg.Sandbox, config.SandboxAgent),
	}

	resolveStep := func(stepName config.StepName, stepFlagAgent, stepFlagModel string) config.StepSelection {
		stepCfg := cfg.Steps[stepName]
		agent := firstAgent(config.AgentName(stepFlagAgent), stepCfg.Agent, defaults.agent)
		model := firstString(stepFlagModel, stepCfg.Model, defaults.model)
		sandbox := firstSandbox(stepCfg.Sandbox, defaults.sandbox)
		return config.StepSelection{Agent: agent, Model: model, Sandbox: sandbox}
	}

	work := resolveStep(config.StepWork, flags.WorkAgent, flags.WorkModel)
	review := resolveStep(config.StepReview, flags.ReviewAgent, flags.ReviewModel)
	gate := resolveStep(config.StepGate, flags.GateAgent, flags.GateModel)

	// iterate falls back to work when neither flag nor per-step config set it.
	iterate := resolveStep(config.StepIterate, flags.IterateAgent, flags.IterateModel)
	if flags.IterateAgent == "" && cfg.Steps[config.StepIterate].Agent == "" {
		iterate.Agent = work.Agent
	}
	if flags.IterateModel == "" && cfg.Steps[config.StepIterate].Model == "" {
		iterate.Model = work.Model
	}
	if cfg.Steps[config.StepIterate].Sandbox == "" {
		iterate.Sandbox = work.Sandbox
	}

	// ralph falls back to gate.
	ralph := resolveStep(config.StepRalph, flags.RalphAgent, flags.RalphModel)
	if flags.RalphAgent == "" && cfg.Steps[config.StepRalph].Agent == "" {
		ralph.Agent = gate.Agent
	}
	if flags.RalphModel == "" && cfg.Steps[config.StepRalph].Model == "" {
		ralph.Model = gate.Model
	}
	if cfg.Steps[config.StepRalph].Sandbox == "" {
		ralph.Sandbox = gate.Sandbox
	}

	return map[config.StepName]config.StepSelection{
		config.StepWork:    work,
		config.StepReview:  review,
		config.StepGate:    gate,
		config.StepIterate: iterate,
		config.StepRalph:   ralph,
	}
}

func firstString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstAgent(values ...config.AgentName) config.AgentName {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstSandbox(values ...config.SandboxMode) config.SandboxMode {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
