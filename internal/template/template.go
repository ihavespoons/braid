// Package template renders BRAID.md with Go text/template, threading
// per-iteration context (step, iteration, prompt, last message, etc.)
// into the prompt sent to agents.
package template

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	texttemplate "text/template"
)

// LoopContext holds the variables exposed to BRAID.md.
// Field names match the variables used inside BRAID.md (e.g. {{.Step}}).
type LoopContext struct {
	Step            string
	Prompt          string
	LastMessage     string
	Iteration       int
	MaxIterations   int
	LogFile         string
	RalphIteration  int
	MaxRalph        int
	RepeatPass      int
	MaxRepeatPasses int
}

// DefaultBraidMD is the template shipped with braid. It uses Go's
// text/template syntax ({{.Var}}).
const DefaultBraidMD = `# BRAID.md

## Project Instructions

## Agent Loop

Step: **{{.Step}}** | Iteration: {{.Iteration}}/{{.MaxIterations}}

### Task
{{.Prompt}}
{{if .LastMessage}}
### Previous Output
{{.LastMessage}}
{{end}}
### History
Session log: {{.LogFile}}
Read the session log for full context from previous steps.
`

// Load returns the contents of BRAID.md from projectRoot, falling back to
// DefaultBraidMD when the file is absent.
func Load(projectRoot string) (string, error) {
	path := filepath.Join(projectRoot, "BRAID.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultBraidMD, nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

// cache memoizes parsed templates keyed by source text. Parsing each template
// once avoids re-parsing on every iteration of a long loop.
type cache struct {
	mu sync.Mutex
	m  map[string]*texttemplate.Template
}

var templateCache = &cache{m: map[string]*texttemplate.Template{}}

func (c *cache) get(src string) (*texttemplate.Template, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.m[src]; ok {
		return t, nil
	}
	t, err := texttemplate.New("braid.md").Parse(src)
	if err != nil {
		return nil, err
	}
	c.m[src] = t
	return t, nil
}

// Render executes src as a Go text/template against ctx and returns the
// rendered string.
func Render(src string, ctx LoopContext) (string, error) {
	t, err := templateCache.get(src)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w\nHint: braid uses Go text/template syntax ({{.Var}})", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}
	return buf.String(), nil
}
