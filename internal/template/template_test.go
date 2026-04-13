package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != DefaultBraidMD {
		t.Error("expected default template when BRAID.md missing")
	}
}

func TestLoad_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	custom := "# custom braid\n{{.Prompt}}"
	if err := os.WriteFile(filepath.Join(dir, "BRAID.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != custom {
		t.Errorf("got %q, want %q", got, custom)
	}
}

func TestRender_DefaultTemplate(t *testing.T) {
	ctx := LoopContext{
		Step:          "work",
		Iteration:     1,
		MaxIterations: 3,
		Prompt:        "fix the bug",
		LastMessage:   "",
		LogFile:       ".braid/logs/2026-04-13.md",
	}
	out, err := Render(DefaultBraidMD, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Step: **work**") {
		t.Errorf("missing step header: %s", out)
	}
	if !strings.Contains(out, "1/3") {
		t.Errorf("missing iteration counter: %s", out)
	}
	if !strings.Contains(out, "fix the bug") {
		t.Errorf("missing prompt: %s", out)
	}
	// Empty LastMessage should skip the Previous Output block.
	if strings.Contains(out, "Previous Output") {
		t.Errorf("should not render Previous Output for empty LastMessage: %s", out)
	}
}

func TestRender_WithLastMessage(t *testing.T) {
	ctx := LoopContext{
		Step:          "review",
		Iteration:     2,
		MaxIterations: 3,
		Prompt:        "check quality",
		LastMessage:   "previous step output",
		LogFile:       "log.md",
	}
	out, err := Render(DefaultBraidMD, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Previous Output") {
		t.Error("should render Previous Output when LastMessage is set")
	}
	if !strings.Contains(out, "previous step output") {
		t.Error("should include LastMessage content")
	}
}

func TestRender_ParseError(t *testing.T) {
	_, err := Render("{{.Unclosed", LoopContext{})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "template parse error") {
		t.Errorf("error should mention parse error: %v", err)
	}
}

func TestRender_UnknownField(t *testing.T) {
	// text/template returns an execution error for unknown fields.
	_, err := Render("{{.Nonexistent}}", LoopContext{})
	if err == nil {
		t.Fatal("expected execute error")
	}
}
