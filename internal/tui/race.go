package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// raceRun holds the per-branch state rendered by RaceModel.
type raceRun struct {
	index int
	step  string
	lines []string
	done  bool
	err   string
}

// RaceModel renders parallel compositions (race / vs). Each run has its
// own event stream demultiplexed via LineEvent.RunIndex and StepEvent
// piggybacked with a run-specific phase.
//
// Phase 4 ships the rendering model; Phase 5 wires it into the executor
// when composition is implemented.
type RaceModel struct {
	title string
	runs  []*raceRun

	width  int
	height int

	spinnerFrame int
	spinning     bool

	done     bool
	finalMsg string
}

// NewRaceModel returns a RaceModel with n empty run panels.
func NewRaceModel(title string, n int) *RaceModel {
	runs := make([]*raceRun, n)
	for i := range runs {
		runs[i] = &raceRun{index: i + 1, lines: make([]string, 0, maxVisibleLines)}
	}
	return &RaceModel{title: title, runs: runs, spinning: true}
}

func (m *RaceModel) Init() tea.Cmd { return tick() }

func (m *RaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		if v.String() == "ctrl+c" || v.String() == "q" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height

	case tickMsg:
		if m.spinning {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		}
		if !m.done {
			return m, tick()
		}
		return m, nil

	case LineEvent:
		if r := m.run(v.RunIndex); r != nil {
			r.lines = append(r.lines, v.Text)
			if len(r.lines) > maxVisibleLines {
				r.lines = r.lines[len(r.lines)-maxVisibleLines:]
			}
		}

	case StepEvent:
		// StepEvent doesn't carry a run index today — future Phase 5
		// wiring will emit per-run StepEvents on per-branch emitters.

	case ErrorEvent:
		// Attribute errors to the most-recent run without a done state.
		for i := len(m.runs) - 1; i >= 0; i-- {
			if !m.runs[i].done && m.runs[i].err == "" {
				m.runs[i].err = v.Err
				break
			}
		}

	case DoneEvent:
		m.done = true
		m.spinning = false
		m.finalMsg = styleOK.Render("✓ Composition complete")
		return m, tea.Quit
	}

	return m, nil
}

func (m *RaceModel) View() string {
	var b strings.Builder

	b.WriteString(styleBanner.Render("▰▰ " + m.title + " ▰▰"))
	b.WriteString("\n")

	for _, r := range m.runs {
		status := styleStepMeta.Render("running…")
		if r.done {
			status = styleOK.Render("done")
		}
		if r.err != "" {
			status = styleError.Render("error: " + r.err)
		}
		spinner := " "
		if m.spinning && !r.done && r.err == "" {
			spinner = styleSpinner.Render(spinnerFrames[m.spinnerFrame])
		}
		b.WriteString(fmt.Sprintf("%s %s  %s\n",
			spinner,
			styleStep.Render(fmt.Sprintf("Run %d", r.index)),
			status,
		))
		// Show the most recent line from this run as a breadcrumb.
		if len(r.lines) > 0 {
			b.WriteString(styleOutput.Render(r.lines[len(r.lines)-1]))
			b.WriteString("\n")
		}
	}

	if m.done {
		b.WriteString("\n" + m.finalMsg + "\n")
	} else {
		b.WriteString(styleFooter.Render("press q or Ctrl+C to quit"))
		b.WriteString("\n")
	}
	return b.String()
}

// run returns the raceRun for runIndex (1-based), or nil if out of range.
func (m *RaceModel) run(runIndex int) *raceRun {
	i := runIndex - 1
	if i < 0 || i >= len(m.runs) {
		return nil
	}
	return m.runs[i]
}
