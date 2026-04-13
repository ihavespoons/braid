package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// RetryConfig controls rate-limit retry behavior.
type RetryConfig struct {
	Enabled      bool          `json:"enabled"`
	PollInterval time.Duration `json:"-"`
	MaxWait      time.Duration `json:"-"`
}

// DefaultRetryConfig returns the built-in retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		Enabled:      true,
		PollInterval: 5 * time.Minute,
		MaxWait:      6 * time.Hour,
	}
}

// BraidConfig is the top-level user configuration loaded from .braid/config.json.
type BraidConfig struct {
	Sandbox     SandboxMode
	Env         []string
	Animation   AnimationStyle
	Agent       AgentName
	Model       string
	Permissions string // global default for claude --permission-mode
	Steps       map[StepName]StepAgentConfig
	Retry       RetryConfig
}

// Default returns a BraidConfig populated with built-in defaults.
func Default() BraidConfig {
	return BraidConfig{
		Sandbox:   SandboxAgent,
		Env:       []string{"CLAUDE_CODE_OAUTH_TOKEN"},
		Animation: AnimationStrip,
		Agent:     AgentClaude,
		Steps: map[StepName]StepAgentConfig{
			StepWork:    {},
			StepReview:  {},
			StepGate:    {},
			StepIterate: {},
			StepRalph:   {},
		},
		Retry: DefaultRetryConfig(),
	}
}

// rawConfig mirrors the JSON shape on disk so we can validate each field.
type rawConfig struct {
	Sandbox     string                        `json:"sandbox"`
	Env         []string                      `json:"env"`
	Animation   string                        `json:"animation"`
	Agent       string                        `json:"agent"`
	Model       string                        `json:"model"`
	Permissions string                        `json:"permissions"`
	Steps       map[string]rawStepAgentConfig `json:"steps"`
	Retry       *rawRetryConfig               `json:"retry"`
}

type rawStepAgentConfig struct {
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Sandbox     string `json:"sandbox"`
	Permissions string `json:"permissions"`
}

type rawRetryConfig struct {
	Enabled             *bool    `json:"enabled"`
	PollIntervalMinutes *float64 `json:"pollIntervalMinutes"`
	MaxWaitMinutes      *float64 `json:"maxWaitMinutes"`
}

// Load reads .braid/config.json from projectRoot and returns the parsed
// config. Missing file returns defaults; malformed file returns defaults
// along with a warning error that callers can log but not fatally fail on.
func Load(projectRoot string) (BraidConfig, error) {
	cfg := Default()
	path := filepath.Join(projectRoot, ".braid", "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("malformed .braid/config.json: %w", err)
	}

	// Legacy "none" → "agent".
	var sandboxWarning error
	switch raw.Sandbox {
	case "":
		// leave default
	case "none":
		sandboxWarning = errors.New(`sandbox: "none" is no longer supported, using "agent"`)
		cfg.Sandbox = SandboxAgent
	default:
		if IsValidSandbox(raw.Sandbox) {
			cfg.Sandbox = SandboxMode(raw.Sandbox)
		}
	}

	if IsValidAnimation(raw.Animation) {
		cfg.Animation = AnimationStyle(raw.Animation)
	}
	if IsValidAgent(raw.Agent) {
		cfg.Agent = AgentName(raw.Agent)
	}
	if raw.Model != "" {
		cfg.Model = raw.Model
	}
	if IsValidPermissionMode(raw.Permissions) && raw.Permissions != "" {
		cfg.Permissions = raw.Permissions
	}

	// Merge env (user adds to defaults; dedupe preserving order).
	if len(raw.Env) > 0 {
		seen := map[string]bool{}
		merged := []string{}
		for _, v := range cfg.Env {
			if !seen[v] {
				merged = append(merged, v)
				seen[v] = true
			}
		}
		for _, v := range raw.Env {
			if v != "" && !seen[v] {
				merged = append(merged, v)
				seen[v] = true
			}
		}
		cfg.Env = merged
	}

	if raw.Steps != nil {
		for _, stepName := range []StepName{StepWork, StepReview, StepGate, StepIterate, StepRalph} {
			if rawStep, ok := raw.Steps[string(stepName)]; ok {
				cfg.Steps[stepName] = parseStepAgentConfig(rawStep)
			}
		}
	}

	if raw.Retry != nil {
		if raw.Retry.Enabled != nil {
			cfg.Retry.Enabled = *raw.Retry.Enabled
		}
		if raw.Retry.PollIntervalMinutes != nil && *raw.Retry.PollIntervalMinutes > 0 {
			cfg.Retry.PollInterval = time.Duration(*raw.Retry.PollIntervalMinutes * float64(time.Minute))
		}
		if raw.Retry.MaxWaitMinutes != nil && *raw.Retry.MaxWaitMinutes > 0 {
			cfg.Retry.MaxWait = time.Duration(*raw.Retry.MaxWaitMinutes * float64(time.Minute))
		}
	}

	return cfg, sandboxWarning
}

func parseStepAgentConfig(raw rawStepAgentConfig) StepAgentConfig {
	out := StepAgentConfig{}
	if IsValidAgent(raw.Agent) {
		out.Agent = AgentName(raw.Agent)
	}
	if raw.Model != "" {
		out.Model = raw.Model
	}
	if IsValidSandbox(raw.Sandbox) {
		out.Sandbox = SandboxMode(raw.Sandbox)
	}
	if IsValidPermissionMode(raw.Permissions) && raw.Permissions != "" {
		out.Permissions = raw.Permissions
	}
	return out
}
