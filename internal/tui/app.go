package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickMsg drives the spinner animation.
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// maxVisibleLines bounds the scrollback pane. Older lines are dropped to
// keep the view steady-height and avoid unbounded memory growth during
// long-running agent sessions.
const maxVisibleLines = 20

// AppModel is the bubbletea model for single-run execution.
type AppModel struct {
	title string

	// Current state
	phase     string
	step      string
	agent     string
	model     string
	iteration int
	maxIter   int

	// Scrollback of streamed output lines
	lines []string

	// Status lines appended below the streaming pane
	status []string

	// Last rendered prompt (shown in a folded panel when ShowRequest).
	prompt      string
	showRequest bool

	// Spinner
	spinnerFrame int
	spinning     bool

	// Terminal state
	width  int
	height int

	// Lifecycle
	done     bool
	finalMsg string
}

// WithShowRequest enables rendering of the most-recent prompt in a folded
// panel above the streaming output.
func (m *AppModel) WithShowRequest(v bool) *AppModel {
	m.showRequest = v
	return m
}

// NewAppModel constructs an initial model ready to be Run() by a tea.Program.
func NewAppModel(title string) *AppModel {
	return &AppModel{
		title:    title,
		lines:    make([]string, 0, maxVisibleLines),
		status:   make([]string, 0, 8),
		spinning: true,
	}
}

// Init is the bubbletea entry hook.
func (m *AppModel) Init() tea.Cmd {
	return tick()
}

// Update handles messages from bubbletea and incoming braid events.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "q":
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

	case PhaseEvent:
		m.phase = v.Title
		// Clear per-phase output noise so the next iteration starts fresh.
		m.lines = m.lines[:0]

	case StepEvent:
		m.step = v.Step
		m.agent = v.Agent
		m.model = v.Model
		if v.Iteration > 0 {
			m.iteration = v.Iteration
			m.maxIter = v.MaxIterations
		}
		m.spinning = true

	case LineEvent:
		m.lines = append(m.lines, v.Text)
		if len(m.lines) > maxVisibleLines {
			m.lines = m.lines[len(m.lines)-maxVisibleLines:]
		}

	case PromptEvent:
		m.prompt = v.Text

	case GateEvent:
		line := fmt.Sprintf("Gate: %s", v.Verdict)
		if v.Message != "" {
			line += " — " + v.Message
		}
		switch v.Verdict {
		case "DONE":
			m.status = append(m.status, styleOK.Render("✓ "+line))
		case "MAX_ITERATIONS":
			m.status = append(m.status, styleWarn.Render("⚠ "+line))
		default:
			m.status = append(m.status, styleWarn.Render("⚠ "+line))
		}

	case LogEvent:
		switch v.Level {
		case "warn":
			m.status = append(m.status, styleWarn.Render("⚠ "+v.Text))
		case "error":
			m.status = append(m.status, styleError.Render("✗ "+v.Text))
		default:
			m.status = append(m.status, styleStepMeta.Render("  "+v.Text))
		}

	case WaitingEvent:
		msg := fmt.Sprintf("Rate limited; retry at %s (attempt %d)", v.NextRetryAt.Format("15:04:05"), v.Attempt)
		m.status = append(m.status, styleWarn.Render("⚠ "+msg))

	case RetryEvent:
		m.status = append(m.status, styleStepMeta.Render(fmt.Sprintf("  retrying (attempt %d)...", v.Attempt)))

	case ErrorEvent:
		m.status = append(m.status, styleError.Render("✗ "+v.Err))

	case DoneEvent:
		m.done = true
		m.spinning = false
		switch v.Verdict {
		case "DONE":
			m.finalMsg = styleOK.Render(fmt.Sprintf("✓ Completed in %d iteration(s)", v.Iterations))
		case "MAX_ITERATIONS":
			m.finalMsg = styleWarn.Render(fmt.Sprintf("⚠ Max iterations reached (%d)", v.Iterations))
		case "":
			m.finalMsg = styleOK.Render("✓ Done")
		default:
			m.finalMsg = styleWarn.Render("⚠ " + v.Verdict)
		}
		if v.LogFile != "" {
			m.finalMsg += styleStepMeta.Render("  log: " + v.LogFile)
		}
		return m, tea.Quit
	}

	return m, nil
}

// View renders the model to a string.
func (m *AppModel) View() string {
	var b strings.Builder

	// Banner
	b.WriteString(styleBanner.Render("▰▰ " + m.title + " ▰▰"))
	b.WriteString("\n")

	// Phase header
	if m.phase != "" {
		b.WriteString(stylePhase.Render(m.phase))
		b.WriteString("\n")
	}

	// Step indicator (with spinner)
	if m.step != "" {
		spinner := " "
		if m.spinning {
			spinner = styleSpinner.Render(spinnerFrames[m.spinnerFrame])
		}
		meta := ""
		if m.agent != "" {
			meta = fmt.Sprintf(" — agent=%s", m.agent)
			if m.model != "" {
				meta += " model=" + m.model
			}
		}
		iter := ""
		if m.maxIter > 0 {
			iter = fmt.Sprintf(" [%d/%d]", m.iteration, m.maxIter)
		}
		b.WriteString(fmt.Sprintf("%s %s%s%s\n",
			spinner,
			styleStep.Render(m.step),
			styleStepMeta.Render(meta),
			styleStepMeta.Render(iter),
		))
	}

	// Folded prompt panel (when --show-request is active)
	if m.showRequest && m.prompt != "" {
		b.WriteString(styleStepMeta.Render("── prompt ──"))
		b.WriteString("\n")
		// Show first N lines of the prompt only to keep the panel compact.
		promptLines := strings.Split(m.prompt, "\n")
		const maxPromptLines = 8
		for i, line := range promptLines {
			if i >= maxPromptLines {
				b.WriteString(styleStepMeta.Render(fmt.Sprintf("  … (%d more lines)", len(promptLines)-maxPromptLines)))
				b.WriteString("\n")
				break
			}
			b.WriteString(styleStepMeta.Render("  " + line))
			b.WriteString("\n")
		}
		b.WriteString(styleStepMeta.Render("────────────"))
		b.WriteString("\n")
	}

	// Streaming output
	for _, line := range m.lines {
		b.WriteString(styleOutput.Render(line))
		b.WriteString("\n")
	}

	// Status lines
	for _, s := range m.status {
		b.WriteString(s)
		b.WriteString("\n")
	}

	// Footer
	if m.done {
		b.WriteString("\n")
		b.WriteString(m.finalMsg)
		b.WriteString("\n")
	} else {
		b.WriteString(styleFooter.Render("press q or Ctrl+C to quit"))
		b.WriteString("\n")
	}

	return b.String()
}
