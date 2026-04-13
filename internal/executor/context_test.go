package executor

import (
	"testing"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
)

func TestResolveStepConfig_AllDefaults(t *testing.T) {
	cfg := config.Default()
	flags := &ast.ParsedFlags{}
	got := ResolveStepConfig(&cfg, flags)

	work := got[config.StepWork]
	if work.Agent != config.AgentClaude {
		t.Errorf("work agent: got %v, want claude", work.Agent)
	}
	if work.Sandbox != config.SandboxAgent {
		t.Errorf("work sandbox: got %v, want agent", work.Sandbox)
	}
	if work.Model != "" {
		t.Errorf("work model: got %q, want empty", work.Model)
	}
}

func TestResolveStepConfig_StepFlagOverridesGlobal(t *testing.T) {
	cfg := config.Default()
	cfg.Agent = config.AgentClaude
	flags := &ast.ParsedFlags{
		Agent:     "claude",
		WorkAgent: "codex",
	}
	got := ResolveStepConfig(&cfg, flags)

	if got[config.StepWork].Agent != config.AgentCodex {
		t.Errorf("work-agent flag should override global, got %v", got[config.StepWork].Agent)
	}
	if got[config.StepReview].Agent != config.AgentClaude {
		t.Errorf("review should inherit global, got %v", got[config.StepReview].Agent)
	}
}

func TestResolveStepConfig_IterateFallsBackToWork(t *testing.T) {
	cfg := config.Default()
	flags := &ast.ParsedFlags{
		WorkAgent: "codex",
		WorkModel: "gpt-5",
	}
	got := ResolveStepConfig(&cfg, flags)

	iterate := got[config.StepIterate]
	if iterate.Agent != config.AgentCodex {
		t.Errorf("iterate should fall back to work agent, got %v", iterate.Agent)
	}
	if iterate.Model != "gpt-5" {
		t.Errorf("iterate should fall back to work model, got %q", iterate.Model)
	}
}

func TestResolveStepConfig_RalphFallsBackToGate(t *testing.T) {
	cfg := config.Default()
	flags := &ast.ParsedFlags{
		GateAgent: "codex",
		GateModel: "gpt-5",
	}
	got := ResolveStepConfig(&cfg, flags)

	ralph := got[config.StepRalph]
	if ralph.Agent != config.AgentCodex {
		t.Errorf("ralph should fall back to gate agent, got %v", ralph.Agent)
	}
	if ralph.Model != "gpt-5" {
		t.Errorf("ralph should fall back to gate model, got %q", ralph.Model)
	}
}

func TestResolveStepConfig_ExplicitStepConfigWins(t *testing.T) {
	cfg := config.Default()
	cfg.Steps[config.StepReview] = config.StepAgentConfig{
		Agent: config.AgentCodex,
		Model: "gpt-5",
	}
	flags := &ast.ParsedFlags{}
	got := ResolveStepConfig(&cfg, flags)

	if got[config.StepReview].Agent != config.AgentCodex {
		t.Errorf("review should use per-step config, got %v", got[config.StepReview].Agent)
	}
}
