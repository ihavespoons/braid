// Package runner provides the AgentRunner interface and implementations
// that spawn AI agents as subprocesses.
package runner

import "strings"

// LineBuffer accumulates byte chunks and emits complete (newline-terminated)
// lines. Any trailing partial line is retained until Flush is called.
type LineBuffer struct {
	partial string
}

// Push appends chunk to the buffer and returns any newly-completed lines.
// Lines are returned without their trailing newline.
func (b *LineBuffer) Push(chunk string) []string {
	b.partial += chunk
	parts := strings.Split(b.partial, "\n")
	// Last element is always the tail (may be empty). Everything before is complete.
	b.partial = parts[len(parts)-1]
	if len(parts) == 1 {
		return nil
	}
	return parts[:len(parts)-1]
}

// Flush returns the remaining partial line (if any) and resets the buffer.
func (b *LineBuffer) Flush() []string {
	if b.partial == "" {
		return nil
	}
	last := b.partial
	b.partial = ""
	return []string{last}
}
