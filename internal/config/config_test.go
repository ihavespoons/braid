package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Agent != AgentClaude {
		t.Errorf("Agent: got %v, want claude", cfg.Agent)
	}
	if cfg.Sandbox != SandboxAgent {
		t.Errorf("Sandbox: got %v, want agent", cfg.Sandbox)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := t.TempDir()
	braidDir := filepath.Join(dir, ".braid")
	if err := os.MkdirAll(braidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"sandbox":"docker","agent":"claude","model":"sonnet-4"}`
	if err := os.WriteFile(filepath.Join(braidDir, "config.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Sandbox != SandboxDocker {
		t.Errorf("Sandbox: got %v", cfg.Sandbox)
	}
	if cfg.Model != "sonnet-4" {
		t.Errorf("Model: got %q", cfg.Model)
	}
}

func TestLoadLegacyNoneSandbox(t *testing.T) {
	dir := t.TempDir()
	braidDir := filepath.Join(dir, ".braid")
	_ = os.MkdirAll(braidDir, 0o755)
	_ = os.WriteFile(filepath.Join(braidDir, "config.json"), []byte(`{"sandbox":"none"}`), 0o644)

	cfg, warn := Load(dir)
	if warn == nil {
		t.Error("expected warning for legacy 'none' sandbox")
	}
	if cfg.Sandbox != SandboxAgent {
		t.Errorf("Sandbox: got %v, want agent", cfg.Sandbox)
	}
}

func TestLoadMalformedReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	braidDir := filepath.Join(dir, ".braid")
	_ = os.MkdirAll(braidDir, 0o755)
	_ = os.WriteFile(filepath.Join(braidDir, "config.json"), []byte(`not json{`), 0o644)

	cfg, err := Load(dir)
	if err == nil {
		t.Error("expected malformed error")
	}
	if cfg.Agent != AgentClaude {
		t.Errorf("should fall back to defaults, got agent %v", cfg.Agent)
	}
}

func TestLoadDockerMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadDocker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Network.Mode != DockerNetworkRestricted {
		t.Errorf("Mode: got %v, want restricted", cfg.Network.Mode)
	}
}

func TestLoadDockerUnrestricted(t *testing.T) {
	dir := t.TempDir()
	braidDir := filepath.Join(dir, ".braid")
	_ = os.MkdirAll(braidDir, 0o755)
	data := `{"network":{"mode":"unrestricted","allowedHosts":["a.example.com","b.example.com"]}}`
	_ = os.WriteFile(filepath.Join(braidDir, "docker.json"), []byte(data), 0o644)

	cfg, err := LoadDocker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Network.Mode != DockerNetworkUnrestricted {
		t.Errorf("Mode: got %v", cfg.Network.Mode)
	}
	if len(cfg.Network.AllowedHosts) != 2 {
		t.Errorf("AllowedHosts: got %v", cfg.Network.AllowedHosts)
	}
}

func TestLoadDockerCustomDockerfile(t *testing.T) {
	dir := t.TempDir()
	braidDir := filepath.Join(dir, ".braid")
	_ = os.MkdirAll(braidDir, 0o755)
	data := `{"dockerfile":".braid/Dockerfile.custom"}`
	_ = os.WriteFile(filepath.Join(braidDir, "docker.json"), []byte(data), 0o644)

	cfg, err := LoadDocker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Dockerfile != ".braid/Dockerfile.custom" {
		t.Errorf("Dockerfile: got %q, want .braid/Dockerfile.custom", cfg.Dockerfile)
	}
}
