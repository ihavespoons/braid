package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type raceRun struct {
	index    int
	step     string
	agent    string
	model    string
	iter     int
	maxIter  int
	verdict  string
	state    stepState
	lines    []string
	viewport viewport.Model
	vpInit   bool
	done     bool
	err      string
}

// RaceModel renders parallel compositions (race / vs). Each branch gets
// its own panel with a header and scrollable viewport. Events are
// demultiplexed by RunIndex (1-based).
type RaceModel struct {
	title string
	runs  []*raceRun

	width  int
	height int

	spinnerFrame int
	spinning     bool

	showHelp bool

	done     bool
	finalMsg string
}

func NewRaceModel(title string, n int) *RaceModel {
	runs := make([]*raceRun, n)
	for i := range runs {
		runs[i] = &raceRun{
			index: i + 1,
			lines: make([]string, 0, 256),
			state: stepRunning,
		}
	}
	return &RaceModel{title: title, runs: runs, spinning: true}
}

func (m *RaceModel) Init() tea.Cmd { return tick() }

func (m *RaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.resizePanels()
		for _, r := range m.runs {
			m.refreshRun(r)
		}

	case tickMsg:
		if m.spinning {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		}
		if !m.done {
			cmds = append(cmds, tick())
		}

	case LineEvent:
		if r := m.run(v.RunIndex); r != nil {
			r.lines = append(r.lines, v.Text)
			if len(r.lines) > maxScrollback {
				r.lines = r.lines[len(r.lines)-maxScrollback:]
			}
			m.refreshRun(r)
		}

	case StepEvent:
		if r := m.run(v.RunIndex); r != nil {
			r.step = v.Step
			r.agent = v.Agent
			r.model = v.Model
			r.iter = v.Iteration
			r.maxIter = v.MaxIterations
			r.state = stepRunning
		}

	case GateEvent:
		if r := m.run(v.RunIndex); r != nil {
			r.verdict = v.Verdict
			switch v.Verdict {
			case "DONE":
				r.state = stepDone
			case "MAX_ITERATIONS":
				r.state = stepWarn
			default:
				r.state = stepDone
			}
			line := fmt.Sprintf("Gate: %s", v.Verdict)
			if v.Message != "" {
				line += " — " + v.Message
			}
			r.lines = append(r.lines, line)
			m.refreshRun(r)
		}

	case ErrorEvent:
		for i := len(m.runs) - 1; i >= 0; i-- {
			if !m.runs[i].done && m.runs[i].err == "" {
				m.runs[i].err = v.Err
				m.runs[i].state = stepError
				break
			}
		}

	case DoneEvent:
		m.done = true
		m.spinning = false
		m.finalMsg = styleOK.Render("✓ Composition complete")
		return m, tea.Quit
	}

	return m, tea.Batch(cmds...)
}

func (m *RaceModel) run(runIndex int) *raceRun {
	i := runIndex - 1
	if i < 0 || i >= len(m.runs) {
		return nil
	}
	return m.runs[i]
}

func (m *RaceModel) resizePanels() {
	if m.width == 0 || m.height == 0 {
		return
	}
	n := len(m.runs)
	if n == 0 {
		return
	}
	bannerH := 2
	statusH := 1
	headerH := 3 // panel header lines
	contentH := m.height - bannerH - statusH - headerH
	if contentH < 3 {
		contentH = 3
	}
	panelW := m.width / n
	if panelW < 20 {
		panelW = 20
	}
	innerW := panelW - 2 // padding
	if innerW < 10 {
		innerW = 10
	}
	for _, r := range m.runs {
		if !r.vpInit {
			r.viewport = viewport.New(innerW, contentH)
			r.viewport.MouseWheelEnabled = true
			r.vpInit = true
		} else {
			r.viewport.Width = innerW
			r.viewport.Height = contentH
		}
	}
}

func (m *RaceModel) refreshRun(r *raceRun) {
	if !r.vpInit {
		return
	}
	atBottom := r.viewport.AtBottom()
	r.viewport.SetContent(wrapLines(r.lines, r.viewport.Width))
	if atBottom {
		r.viewport.GotoBottom()
	}
}

func (m *RaceModel) View() string {
	if m.width == 0 || m.height == 0 {
		return styleBanner.Render("▰▰ " + m.title + " ▰▰")
	}

	banner := styleBannerBar.Width(m.width).Render("▰▰ " + m.title + " ▰▰")

	panels := make([]string, len(m.runs))
	panelW := m.width / len(m.runs)
	if panelW < 20 {
		panelW = 20
	}
	for i, r := range m.runs {
		panels[i] = m.renderPanel(r, panelW)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	status := m.renderStatusBar()
	view := lipgloss.JoinVertical(lipgloss.Left, banner, body, status)
	if m.showHelp {
		help := styleHelpOverlay.Render(strings.Join([]string{
			styleSidebarTitle.Render("Keybindings"),
			"",
			"q, ctrl+c    quit",
			"?            toggle this help",
			"mouse wheel  scroll panel",
		}, "\n"))
		return overlay(view, help, m.width, m.height)
	}
	return view
}

func (m *RaceModel) renderPanel(r *raceRun, width int) string {
	var icon string
	switch r.state {
	case stepDone:
		icon = styleStepDone.Render("✓")
	case stepWarn:
		icon = styleStepWarn.Render("⚠")
	case stepError:
		icon = styleStepError.Render("✗")
	default:
		if m.spinning {
			icon = styleSpinner.Render(spinnerFrames[m.spinnerFrame])
		} else {
			icon = " "
		}
	}
	title := fmt.Sprintf("Run %d", r.index)
	if r.step != "" {
		title += " — " + r.step
	}
	if r.maxIter > 0 {
		title += fmt.Sprintf(" [%d/%d]", r.iter, r.maxIter)
	}
	header := icon + " " + styleStep.Render(title)

	meta := ""
	if r.agent != "" {
		meta = styleStepMeta.Render(fmt.Sprintf("agent=%s", r.agent))
		if r.model != "" {
			meta += styleStepMeta.Render(" model=" + r.model)
		}
	}
	if r.err != "" {
		meta = styleError.Render("error: " + r.err)
	} else if r.verdict != "" {
		switch r.verdict {
		case "DONE":
			meta += "  " + styleOK.Render("DONE")
		case "MAX_ITERATIONS":
			meta += "  " + styleWarn.Render("MAX_ITERATIONS")
		default:
			meta += "  " + styleStepMeta.Render(r.verdict)
		}
	}

	body := ""
	if r.vpInit {
		body = r.viewport.View()
	}

	panel := lipgloss.JoinVertical(lipgloss.Left, header, meta, body)
	return styleSidebar.Width(width).Render(panel)
}

func (m *RaceModel) renderStatusBar() string {
	left := ""
	if m.done {
		left = m.finalMsg
	} else {
		spinner := " "
		if m.spinning {
			spinner = styleSpinner.Render(spinnerFrames[m.spinnerFrame])
		}
		left = spinner + " composition running"
	}
	right := styleFooter.Render("q quit · ? help")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW - 2
	if gap < 1 {
		gap = 1
	}
	return styleStatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}
