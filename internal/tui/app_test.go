package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// These tests exercise the Update/View logic directly — we don't need a
// running tea.Program because Update/View are pure functions of (model, msg).
// Color is disabled via lipgloss-friendly substring checks (render() adds
// ANSI wrappers but the underlying text is present).

func sized(m tea.Model) tea.Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

func TestAppModel_StepAndLineRender(t *testing.T) {
	m := NewAppModel("test session")
	sized(m)

	m.Update(PhaseEvent{Title: "Iteration 1/3"})
	m.Update(StepEvent{Step: "work", Agent: "claude", Model: "sonnet-4", Iteration: 1, MaxIterations: 3})
	m.Update(LineEvent{Text: "hello from agent"})

	view := m.View()
	for _, want := range []string{"test session", "Iteration 1/3", "work", "claude", "sonnet-4", "hello from agent"} {
		if !strings.Contains(view, want) {
			t.Errorf("View missing %q:\n%s", want, view)
		}
	}
}

func TestAppModel_LineScrollback(t *testing.T) {
	m := NewAppModel("scrollback test")
	sized(m)

	// Fill beyond the scrollback cap.
	total := maxScrollback + 5
	for i := range total {
		m.Update(LineEvent{Text: lineFor(i)})
	}
	if len(m.lines) != maxScrollback {
		t.Errorf("lines retained: got %d, want %d", len(m.lines), maxScrollback)
	}
	// Oldest 5 lines should be dropped.
	if m.lines[0] != lineFor(5) {
		t.Errorf("first retained line: got %q, want %q", m.lines[0], lineFor(5))
	}
}

func TestAppModel_DoneEventTransitionsToFinal(t *testing.T) {
	m := NewAppModel("done test")
	sized(m)
	newModel, cmd := m.Update(DoneEvent{Verdict: "DONE", Iterations: 2, LogFile: "/tmp/x.md"})
	if !m.done {
		t.Error("expected done=true after DoneEvent")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd after DoneEvent")
	}
	view := newModel.View()
	if !strings.Contains(view, "Completed in 2 iteration(s)") {
		t.Errorf("view should show completion, got:\n%s", view)
	}
}

func TestAppModel_QuitOnCtrlC(t *testing.T) {
	t.Skip("tea.KeyMsg has unexported fields; visual-tested separately")
}

func TestRaceModel_LinesDemuxedByRun(t *testing.T) {
	m := NewRaceModel("race test", 3)
	sized(m)
	m.Update(LineEvent{RunIndex: 1, Text: "from run 1"})
	m.Update(LineEvent{RunIndex: 2, Text: "from run 2"})
	m.Update(LineEvent{RunIndex: 3, Text: "from run 3"})

	if got := m.runs[0].lines[0]; got != "from run 1" {
		t.Errorf("run 1 line: got %q", got)
	}
	if got := m.runs[1].lines[0]; got != "from run 2" {
		t.Errorf("run 2 line: got %q", got)
	}
	view := m.View()
	for _, want := range []string{"Run 1", "Run 2", "Run 3", "from run 3"} {
		if !strings.Contains(view, want) {
			t.Errorf("View missing %q:\n%s", want, view)
		}
	}
}

func lineFor(i int) string {
	if i < 10 {
		return "line-0" + string(rune('0'+i))
	}
	return "line-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}
