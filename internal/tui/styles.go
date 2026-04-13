package tui

import "github.com/charmbracelet/lipgloss"

// Colors are 256-color ANSI codes so the palette works in both dark and
// light terminals without adaptive style overhead.
var (
	colorPrimary = lipgloss.Color("69")  // soft blue
	colorAccent  = lipgloss.Color("212") // pink
	colorSuccess = lipgloss.Color("42")  // green
	colorWarn    = lipgloss.Color("214") // amber
	colorError   = lipgloss.Color("196") // red
	colorMuted   = lipgloss.Color("244") // grey
	colorText    = lipgloss.Color("252") // near-white
	colorBgSoft  = lipgloss.Color("236") // dark grey for status bar
)

var (
	styleBanner = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)

	stylePhase = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleStep = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleStepMeta = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleOutput = lipgloss.NewStyle().
			Foreground(colorText)

	styleOK = lipgloss.NewStyle().
		Foreground(colorSuccess).
		Bold(true)

	styleWarn = lipgloss.NewStyle().
			Foreground(colorWarn).
			Bold(true)

	styleError = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	styleSpinner = lipgloss.NewStyle().
			Foreground(colorAccent)

	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSidebar = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(colorMuted).
			Padding(0, 1)

	styleMain = lipgloss.NewStyle().
			Padding(0, 1)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorBgSoft).
			Padding(0, 1)

	styleSidebarTitle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleStepDone    = styleOK
	styleStepRunning = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	styleStepWarn    = styleWarn
	styleStepError   = styleError
	styleStepPending = styleStepMeta

	styleHelpOverlay = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(1, 2).
				Background(colorBgSoft).
				Foreground(colorText)

	styleBannerBar = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(colorMuted).
			Padding(0, 1)

	stylePromptPanel = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorMuted).
				Foreground(colorText).
				Padding(0, 1)
)

// spinnerFrames are the animation frames used during active agent calls.
// Braille-style spinners keep the motion subtle.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
