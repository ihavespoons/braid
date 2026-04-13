package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session writes per-run markdown logs into .braid/logs/. A session is
// created once and reused across steps so iterations accumulate in one file.
type Session struct {
	Path string
	mu   sync.Mutex
	file *os.File
}

// NewSession creates a new session log at .braid/logs/<timestamp>.md.
// The file is created lazily on first write to avoid empty logs for
// runs that fail before any step completes.
func NewSession(projectRoot string) (*Session, error) {
	logsDir := filepath.Join(projectRoot, ".braid", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating logs dir: %w", err)
	}
	name := time.Now().Format("2006-01-02-150405") + ".md"
	return &Session{Path: filepath.Join(logsDir, name)}, nil
}

// Append writes a new entry for the given step and iteration.
func (s *Session) Append(step string, iteration int, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening session log: %w", err)
		}
		s.file = f
	}

	ts := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("## [%s %d] %s\n\n%s\n\n---\n\n", step, iteration, ts, content)
	if _, err := s.file.WriteString(entry); err != nil {
		return fmt.Errorf("writing session log: %w", err)
	}
	return nil
}

// Close releases the underlying file handle.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
