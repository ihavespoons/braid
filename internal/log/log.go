// Package log provides colored console logging used before the bubbletea TUI
// takes over, and shared styles for log rendering.
package log

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI color codes. Kept minimal — TUI rendering uses lipgloss.
const (
	ansiReset  = "\x1b[0m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiBold   = "\x1b[1m"
)

// Writer is the destination for all log output. Defaults to os.Stderr.
var Writer io.Writer = os.Stderr

// EnableColor controls whether ANSI color codes are emitted. Callers may
// disable it when writing to non-TTY destinations.
var EnableColor = true

func colorize(code, s string) string {
	if !EnableColor {
		return s
	}
	return code + s + ansiReset
}

// Phase prints a bold blue phase header surrounded by decorative borders.
func Phase(title string) {
	border := strings.Repeat("─", len(title)+4)
	fmt.Fprintln(Writer, colorize(ansiBlue, "┌"+border+"┐"))
	fmt.Fprintln(Writer, colorize(ansiBlue, "│  ")+colorize(ansiBold+ansiBlue, title)+colorize(ansiBlue, "  │"))
	fmt.Fprintln(Writer, colorize(ansiBlue, "└"+border+"┘"))
}

// Step prints a cyan step indicator.
func Step(format string, args ...any) {
	fmt.Fprintln(Writer, colorize(ansiCyan, "▸ ")+fmt.Sprintf(format, args...))
}

// OK prints a green success line.
func OK(format string, args ...any) {
	fmt.Fprintln(Writer, colorize(ansiGreen, "✓ ")+fmt.Sprintf(format, args...))
}

// Warn prints a yellow warning line.
func Warn(format string, args ...any) {
	fmt.Fprintln(Writer, colorize(ansiYellow, "⚠ ")+fmt.Sprintf(format, args...))
}

// Error prints a red error line.
func Error(format string, args ...any) {
	fmt.Fprintln(Writer, colorize(ansiRed, "✗ ")+fmt.Sprintf(format, args...))
}

// Info prints an uncolored informational line.
func Info(format string, args ...any) {
	fmt.Fprintln(Writer, fmt.Sprintf(format, args...))
}
