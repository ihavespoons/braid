package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// tickMsg drives the spinner animation.
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// maxScrollback bounds the in-memory output history. The viewport renders
// from this slice; older lines drop off the front when the cap is hit.
const maxScrollback = 5000

// promptPanelRows is the fixed height (in terminal rows) reserved for the
// prompt panel when it is visible. Using a fixed height keeps the sidebar
// and main viewport aligned regardless of how the prompt wraps.
const promptPanelRows = 10

type stepState int

const (
	stepRunning stepState = iota
	stepDone
	stepWarn
	stepError
)

type stepEntry struct {
	name      string
	agent     string
	model     string
	iteration int
	maxIter   int
	state     stepState
	verdict   string
}

// AppModel is the bubbletea model for single-run execution.
type AppModel struct {
	title string

	phase  string
	steps  []stepEntry
	prompt string

	lines []string

	retryCount int
	waiting    *WaitingEvent
	lastError  string

	spinnerFrame int
	spinning     bool

	width  int
	height int

	viewport     viewport.Model
	viewportInit bool
	showPrompt   bool
	showHelp     bool

	done     bool
	verdict  string
	finalMsg string
}

// WithShowRequest sets the initial visibility of the prompt panel. Users
// can also toggle it interactively with `p`.
func (m *AppModel) WithShowRequest(v bool) *AppModel {
	m.showPrompt = v
	return m
}

func NewAppModel(title string) *AppModel {
	return &AppModel{
		title:    title,
		lines:    make([]string, 0, 256),
		steps:    make([]stepEntry, 0, 16),
		spinning: true,
	}
}

func (m *AppModel) Init() tea.Cmd {
	return tick()
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "p":
			m.showPrompt = !m.showPrompt
			m.resizeViewport()
			m.refreshViewport()
			return m, nil
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.resizeViewport()
		m.refreshViewport()

	case tea.MouseMsg:
		// Forward to viewport for scroll-wheel handling.

	case tickMsg:
		if m.spinning {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		}
		if !m.done {
			cmds = append(cmds, tick())
		}

	case PhaseEvent:
		m.phase = v.Title

	case StepEvent:
		m.appendStep(v)
		m.spinning = true

	case LineEvent:
		m.appendLine(v.Text)

	case PromptEvent:
		m.prompt = v.Text
		if m.showPrompt {
			m.resizeViewport()
			m.refreshViewport()
		}

	case GateEvent:
		m.markGate(v)

	case LogEvent:
		switch v.Level {
		case "warn":
			m.appendLine(styleWarn.Render("⚠ " + v.Text))
		case "error":
			m.appendLine(styleError.Render("✗ " + v.Text))
			m.lastError = v.Text
		default:
			m.appendLine(styleStepMeta.Render("  " + v.Text))
		}

	case WaitingEvent:
		w := v
		m.waiting = &w

	case RetryEvent:
		m.retryCount = v.Attempt
		m.waiting = nil

	case ErrorEvent:
		m.lastError = v.Err
		m.appendLine(styleError.Render("✗ " + v.Err))
		if n := len(m.steps); n > 0 {
			m.steps[n-1].state = stepError
		}

	case DoneEvent:
		m.done = true
		m.spinning = false
		m.verdict = v.Verdict
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

	// Forward key/mouse events to the viewport for scroll bindings.
	// Auto-tail is handled inside refreshViewport, which captures the
	// pre-update follow state before mutating content.
	if m.viewportInit {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		if vpCmd != nil {
			cmds = append(cmds, vpCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *AppModel) appendStep(e StepEvent) {
	entry := stepEntry{
		name:      e.Step,
		agent:     e.Agent,
		model:     e.Model,
		iteration: e.Iteration,
		maxIter:   e.MaxIterations,
		state:     stepRunning,
	}
	// Mark the previous step done if it was still running.
	if n := len(m.steps); n > 0 && m.steps[n-1].state == stepRunning {
		m.steps[n-1].state = stepDone
	}
	m.steps = append(m.steps, entry)
}

func (m *AppModel) markGate(g GateEvent) {
	n := len(m.steps)
	if n == 0 {
		return
	}
	s := &m.steps[n-1]
	s.verdict = g.Verdict
	switch g.Verdict {
	case "DONE":
		s.state = stepDone
	case "MAX_ITERATIONS":
		s.state = stepWarn
	default:
		s.state = stepDone
	}
	line := fmt.Sprintf("Gate: %s", g.Verdict)
	if g.Message != "" {
		line += " — " + g.Message
	}
	switch g.Verdict {
	case "DONE":
		m.appendLine(styleOK.Render("✓ " + line))
	case "MAX_ITERATIONS":
		m.appendLine(styleWarn.Render("⚠ " + line))
	default:
		m.appendLine(styleStepMeta.Render("• " + line))
	}
}

func (m *AppModel) appendLine(s string) {
	m.lines = append(m.lines, s)
	if len(m.lines) > maxScrollback {
		m.lines = m.lines[len(m.lines)-maxScrollback:]
	}
	m.refreshViewport()
}

// resizeViewport recomputes viewport dimensions from current width/height
// and the visibility of the prompt panel.
func (m *AppModel) resizeViewport() {
	if m.width == 0 || m.height == 0 {
		return
	}
	sw := sidebarWidth(m.width)
	mainW := m.width - sw - 2 // sidebar border + main padding
	if mainW < 10 {
		mainW = 10
	}
	contentH := m.contentHeight()
	if !m.viewportInit {
		m.viewport = viewport.New(mainW, contentH)
		m.viewport.MouseWheelEnabled = true
		m.viewportInit = true
	} else {
		m.viewport.Width = mainW
		m.viewport.Height = contentH
	}
}

func (m *AppModel) refreshViewport() {
	if !m.viewportInit {
		return
	}
	// Capture follow-state before SetContent: if the user was already
	// pinned to the bottom, we want to stay pinned after appending. If
	// they manually scrolled up, leave their position alone.
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(wrapLines(m.lines, m.viewport.Width))
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// wrapLines hard-wraps each line to width visible columns, preserving any
// embedded ANSI styling. Without this, long file paths and tool output
// overflow the viewport's column and visually bleed into the sidebar.
func wrapLines(lines []string, width int) string {
	if width < 1 {
		width = 1
	}
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(ansi.Hardwrap(line, width, true))
	}
	return sb.String()
}

func (m *AppModel) View() string {
	if m.width == 0 || m.height == 0 {
		// Pre-resize: render a minimal placeholder so the alt-screen
		// has something while we wait for the first WindowSizeMsg.
		return styleBanner.Render("▰▰ " + m.title + " ▰▰")
	}

	if m.showHelp {
		return m.renderWithHelpOverlay()
	}

	banner := m.renderBanner()
	sidebar := m.renderSidebar()
	main := m.renderMain()
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	status := m.renderStatusBar()

	parts := []string{banner}
	if m.showPrompt && m.prompt != "" {
		parts = append(parts, m.renderPromptPanel(m.width))
	}
	parts = append(parts, body, status)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *AppModel) renderBanner() string {
	title := "▰▰ " + m.title + " ▰▰"
	if m.phase != "" {
		title += "   " + stylePhase.Render(m.phase)
	}
	return styleBannerBar.Width(m.width).Render(title)
}

func (m *AppModel) renderSidebar() string {
	sw := sidebarWidth(m.width)
	contentH := m.contentHeight()

	var b strings.Builder
	b.WriteString(styleSidebarTitle.Render("Steps"))
	b.WriteString("\n")
	for _, s := range m.steps {
		b.WriteString(m.renderStepLine(s))
		b.WriteString("\n")
	}

	// Active step meta (agent/model/iteration) for the trailing entry.
	if n := len(m.steps); n > 0 {
		s := m.steps[n-1]
		b.WriteString("\n")
		if s.agent != "" {
			b.WriteString(styleStepMeta.Render("Agent:  ") + s.agent + "\n")
		}
		if s.model != "" {
			b.WriteString(styleStepMeta.Render("Model:  ") + s.model + "\n")
		}
		if s.maxIter > 0 {
			b.WriteString(styleStepMeta.Render("Iter:   ") +
				fmt.Sprintf("%d/%d\n", s.iteration, s.maxIter))
		}
	}
	if m.retryCount > 0 {
		b.WriteString(styleStepMeta.Render("Retries:") +
			fmt.Sprintf(" %d\n", m.retryCount))
	}
	if m.waiting != nil {
		secs := time.Until(m.waiting.NextRetryAt).Round(time.Second).Seconds()
		if secs < 0 {
			secs = 0
		}
		b.WriteString(styleWarn.Render(fmt.Sprintf("Wait:    %.0fs", secs)))
		b.WriteString("\n")
	}

	return styleSidebar.Width(sw).Height(contentH).Render(b.String())
}

func (m *AppModel) renderStepLine(s stepEntry) string {
	var icon string
	switch s.state {
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
			icon = styleStepPending.Render("·")
		}
	}
	label := s.name
	if s.maxIter > 0 {
		label += fmt.Sprintf(" [%d/%d]", s.iteration, s.maxIter)
	}
	switch s.state {
	case stepRunning:
		label = styleStepRunning.Render(label)
	case stepDone:
		label = styleStepDone.Render(label)
	case stepWarn:
		label = styleStepWarn.Render(label)
	case stepError:
		label = styleStepError.Render(label)
	default:
		label = styleStepPending.Render(label)
	}
	return icon + " " + label
}

func (m *AppModel) renderMain() string {
	mw := m.width - sidebarWidth(m.width) - 2
	if mw < 10 {
		mw = 10
	}
	contentH := m.contentHeight()
	return styleMain.Width(mw).Height(contentH).Render(m.viewport.View())
}

// renderPromptPanel returns a panel of exactly promptPanelRows rows so the
// sidebar and main viewport can size themselves predictably regardless of
// how the prompt wraps.
func (m *AppModel) renderPromptPanel(width int) string {
	const visibleLines = 6
	lines := strings.Split(m.prompt, "\n")
	shown := lines
	more := 0
	if len(shown) > visibleLines {
		shown = lines[:visibleLines]
		more = len(lines) - visibleLines
	}
	body := strings.Join(shown, "\n")
	if more > 0 {
		body += fmt.Sprintf("\n… (%d more lines)", more)
	}
	header := styleStepMeta.Render("prompt (toggle with 'p')")
	panel := stylePromptPanel.Width(width - 2).Render(header + "\n" + body)
	return clampToRows(panel, promptPanelRows)
}

// clampToRows truncates or pads s to exactly n visible rows.
func clampToRows(s string, n int) string {
	rows := strings.Split(s, "\n")
	if len(rows) >= n {
		return strings.Join(rows[:n], "\n")
	}
	return s + strings.Repeat("\n", n-len(rows))
}

func (m *AppModel) renderStatusBar() string {
	left := ""
	if m.done {
		left = m.finalMsg
	} else {
		spinner := " "
		if m.spinning {
			spinner = styleSpinner.Render(spinnerFrames[m.spinnerFrame])
		}
		phase := m.phase
		if phase == "" {
			phase = "running"
		}
		left = spinner + " " + phase
		if m.retryCount > 0 {
			left += styleStepMeta.Render(fmt.Sprintf("  retries:%d", m.retryCount))
		}
		if m.waiting != nil {
			secs := time.Until(m.waiting.NextRetryAt).Round(time.Second).Seconds()
			if secs < 0 {
				secs = 0
			}
			left += styleWarn.Render(fmt.Sprintf("  rate-limited %.0fs", secs))
		}
	}
	right := styleFooter.Render("q quit · ↑↓ scroll · p prompt · ? help")

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW - 2
	if gap < 1 {
		gap = 1
	}
	bar := left + strings.Repeat(" ", gap) + right
	return styleStatusBar.Width(m.width).Render(bar)
}

func (m *AppModel) contentHeight() int {
	bannerH := 2
	statusH := 1
	promptH := 0
	if m.showPrompt && m.prompt != "" {
		promptH = promptPanelRows
	}
	h := m.height - bannerH - statusH - promptH
	if h < 3 {
		h = 3
	}
	return h
}

func (m *AppModel) renderWithHelpOverlay() string {
	base := func() string {
		showHelp := m.showHelp
		m.showHelp = false
		s := m.View()
		m.showHelp = showHelp
		return s
	}()
	help := styleHelpOverlay.Render(strings.Join([]string{
		styleSidebarTitle.Render("Keybindings"),
		"",
		"q, ctrl+c    quit",
		"p            toggle prompt panel",
		"?            toggle this help",
		"↑ ↓          scroll one line",
		"pgup pgdn    scroll one page",
		"g G          jump to top / bottom",
		"mouse wheel  scroll",
	}, "\n"))
	return overlay(base, help, m.width, m.height)
}

// overlay places the foreground centered over the background. It returns the
// background unchanged if the foreground would not fit.
func overlay(bg, fg string, w, h int) string {
	fgW := lipgloss.Width(fg)
	fgH := lipgloss.Height(fg)
	if fgW > w || fgH > h {
		return bg
	}
	// lipgloss.Place draws the fg centered on a blank canvas; we then
	// render that canvas on top of the background by replacing the
	// middle rows. For simplicity we just use Place over the bg lines.
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, fg)
}

func sidebarWidth(total int) int {
	w := total / 4
	if w < 22 {
		w = 22
	}
	if w > 36 {
		w = 36
	}
	if w > total/2 {
		w = total / 2
	}
	return w
}

