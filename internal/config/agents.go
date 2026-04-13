// Package config defines braid's configuration schema and loaders.
package config

// AgentName identifies a supported AI code agent.
type AgentName string

const (
	AgentClaude   AgentName = "claude"
	AgentCodex    AgentName = "codex"
	AgentOpenCode AgentName = "opencode"
)

// IsValidAgent reports whether v is a recognized agent name.
func IsValidAgent(v string) bool {
	switch AgentName(v) {
	case AgentClaude, AgentCodex, AgentOpenCode:
		return true
	}
	return false
}

// StepName identifies a role in a braid execution pipeline.
type StepName string

const (
	StepWork    StepName = "work"
	StepReview  StepName = "review"
	StepGate    StepName = "gate"
	StepIterate StepName = "iterate"
	StepRalph   StepName = "ralph"
)

// SandboxMode controls how agents are executed.
type SandboxMode string

const (
	SandboxAgent  SandboxMode = "agent"
	SandboxDocker SandboxMode = "docker"
)

// IsValidSandbox reports whether v is a recognized sandbox mode.
func IsValidSandbox(v string) bool {
	switch SandboxMode(v) {
	case SandboxAgent, SandboxDocker:
		return true
	}
	return false
}

// AnimationStyle selects the TUI idle animation.
type AnimationStyle string

const (
	AnimationFlame    AnimationStyle = "flame"
	AnimationStrip    AnimationStyle = "strip"
	AnimationCampfire AnimationStyle = "campfire"
	AnimationPot      AnimationStyle = "pot"
	AnimationPulse    AnimationStyle = "pulse"
)

// IsValidAnimation reports whether v is a recognized animation style.
func IsValidAnimation(v string) bool {
	switch AnimationStyle(v) {
	case AnimationFlame, AnimationStrip, AnimationCampfire, AnimationPot, AnimationPulse:
		return true
	}
	return false
}

// StepAgentConfig is a per-step override.
type StepAgentConfig struct {
	Agent   AgentName   `json:"agent,omitempty"`
	Model   string      `json:"model,omitempty"`
	Sandbox SandboxMode `json:"sandbox,omitempty"`
}

// StepSelection is the fully-resolved configuration for a single step invocation.
type StepSelection struct {
	Agent   AgentName
	Model   string
	Sandbox SandboxMode
}
