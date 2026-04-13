package runner

import (
	"strings"
	"testing"
)

func TestClaudeStream_SystemInit(t *testing.T) {
	s := NewClaudeStream()
	display, ok := s.Push(`{"type":"system","subtype":"init","model":"claude-sonnet-4","session_id":"abc12345xyz"}`)
	if !ok {
		t.Fatal("expected system init to display")
	}
	if !strings.Contains(display, "abc12345") || !strings.Contains(display, "claude-sonnet-4") {
		t.Errorf("display missing expected fields: %q", display)
	}
}

func TestClaudeStream_AssistantTextAndToolUse(t *testing.T) {
	s := NewClaudeStream()
	line := `{"type":"assistant","message":{"content":[` +
		`{"type":"text","text":"I'll read the file."},` +
		`{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/foo.go"}}` +
		`]}}`
	display, ok := s.Push(line)
	if !ok {
		t.Fatal("expected assistant block to display")
	}
	if !strings.Contains(display, "I'll read the file.") {
		t.Errorf("missing assistant text: %q", display)
	}
	if !strings.Contains(display, "→ Read: /tmp/foo.go") {
		t.Errorf("missing tool_use summary: %q", display)
	}
}

func TestClaudeStream_ToolResultUsesRememberedName(t *testing.T) {
	s := NewClaudeStream()
	_, _ = s.Push(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"ls"}}]}}`)
	display, ok := s.Push(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"a\nb\nc"}]}}`)
	if !ok {
		t.Fatal("expected tool_result to display")
	}
	if !strings.Contains(display, "Bash") {
		t.Errorf("display should mention tool name Bash: %q", display)
	}
	if !strings.Contains(display, "a b c") {
		t.Errorf("display should include flattened result content: %q", display)
	}
}

func TestClaudeStream_ResultCapturesText(t *testing.T) {
	s := NewClaudeStream()
	_, _ = s.Push(`{"type":"result","subtype":"success","is_error":false,"duration_ms":1234,"num_turns":2,"total_cost_usd":0.05,"result":"DONE\nlooks good"}`)
	if got := s.Result(); got != "DONE\nlooks good" {
		t.Errorf("Result(): got %q", got)
	}
}

func TestClaudeStream_InvalidJSONPassesThrough(t *testing.T) {
	s := NewClaudeStream()
	display, ok := s.Push("not json at all")
	if !ok {
		t.Fatal("expected non-JSON line to be displayed verbatim")
	}
	if display != "not json at all" {
		t.Errorf("display: got %q", display)
	}
}

func TestClaudeStream_EmptyLineSkipped(t *testing.T) {
	s := NewClaudeStream()
	if _, ok := s.Push(""); ok {
		t.Error("empty line should not display")
	}
	if _, ok := s.Push("   "); ok {
		t.Error("whitespace-only line should not display")
	}
}
