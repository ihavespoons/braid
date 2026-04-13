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
)

// styleSet bundles all lipgloss styles used by the TUI. Instances are
// cheap — lipgloss styles are immutable value types.
var (
	styleBanner = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 2)

	stylePhase = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			MarginTop(1)

	styleStep = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleStepMeta = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleOutput = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(2)

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
			Foreground(colorMuted).
			MarginTop(1)
)

// spinnerFrames are the animation frames used during active agent calls.
// Braille-style spinners keep the motion subtle.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
