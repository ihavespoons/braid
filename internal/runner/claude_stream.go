package runner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Display palette for parsed claude stream events. Lipgloss auto-detects
// the output's color profile and degrades to plain text when stdout is
// not a TTY, so these styles are safe in piped/log contexts too.
var (
	csColorPrimary = lipgloss.Color("69")  // blue
	csColorAccent  = lipgloss.Color("212") // pink
	csColorOK      = lipgloss.Color("42")  // green
	csColorErr     = lipgloss.Color("196") // red
	csColorMuted   = lipgloss.Color("244") // grey
	csColorPath    = lipgloss.Color("117") // cyan-ish for file paths
	csColorCmd     = lipgloss.Color("180") // amber-ish for shell commands

	csArrowOut    = lipgloss.NewStyle().Foreground(csColorPrimary).Bold(true).Render("→")
	csArrowIn     = lipgloss.NewStyle().Foreground(csColorPrimary).Bold(true).Render("←")
	csBullet      = lipgloss.NewStyle().Foreground(csColorAccent).Bold(true).Render("●")
	csCheck       = lipgloss.NewStyle().Foreground(csColorOK).Bold(true).Render("✓")
	csCross       = lipgloss.NewStyle().Foreground(csColorErr).Bold(true).Render("✗")
	csStyleTool   = lipgloss.NewStyle().Foreground(csColorAccent).Bold(true)
	csStyleMuted  = lipgloss.NewStyle().Foreground(csColorMuted)
	csStylePath   = lipgloss.NewStyle().Foreground(csColorPath).Underline(true)
	csStyleCmd    = lipgloss.NewStyle().Foreground(csColorCmd)
	csStyleResult = lipgloss.NewStyle().Foreground(csColorMuted).Italic(true)
	csStyleText   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	csStyleThink  = lipgloss.NewStyle().Foreground(csColorMuted).Italic(true)
	csStyleDone   = lipgloss.NewStyle().Foreground(csColorOK).Bold(true)
)

// ClaudeStream parses claude's `--output-format stream-json` output. It
// turns each JSONL event into a human-readable display line for the TUI
// and extracts the final assistant `result` text so the executor still
// receives the agent's actual reply (not the raw JSONL).
//
// Lines that fail to parse as JSON are returned verbatim, so non-JSON
// chatter (e.g. setup banners, error messages) still reaches the user.
type ClaudeStream struct {
	result    string
	toolNames map[string]string // tool_use_id -> tool name
}

func NewClaudeStream() *ClaudeStream {
	return &ClaudeStream{toolNames: make(map[string]string)}
}

// Push parses one line. Returns (display, true) when the TUI should show
// something, or ("", false) for events that carry no display value.
// Invalid-JSON lines round-trip as (line, true).
func (c *ClaudeStream) Push(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return line, true
	}
	switch t, _ := ev["type"].(string); t {
	case "system":
		if sub, _ := ev["subtype"].(string); sub == "init" {
			model, _ := ev["model"].(string)
			sid, _ := ev["session_id"].(string)
			if len(sid) > 8 {
				sid = sid[:8]
			}
			parts := []string{csBullet + " session " + csStyleTool.Render(sid)}
			if model != "" {
				parts = append(parts, csStyleMuted.Render("model="+model))
			}
			return strings.Join(parts, " · "), true
		}
	case "assistant":
		return c.formatAssistant(ev)
	case "user":
		return c.formatUser(ev)
	case "result":
		if r, _ := ev["result"].(string); r != "" {
			c.result = r
		}
		dur, _ := ev["duration_ms"].(float64)
		turns, _ := ev["num_turns"].(float64)
		cost, _ := ev["total_cost_usd"].(float64)
		summary := fmt.Sprintf("done in %.1fs · %.0f turns · $%.4f", dur/1000, turns, cost)
		return csBullet + " " + csStyleDone.Render(summary), true
	}
	return "", false
}

// Result returns the final assistant text once the "result" event has
// been seen. Empty until then.
func (c *ClaudeStream) Result() string {
	return c.result
}

func (c *ClaudeStream) formatAssistant(ev map[string]any) (string, bool) {
	msg, _ := ev["message"].(map[string]any)
	if msg == nil {
		return "", false
	}
	content, _ := msg["content"].([]any)
	var out []string
	for _, block := range content {
		b, _ := block.(map[string]any)
		if b == nil {
			continue
		}
		switch bt, _ := b["type"].(string); bt {
		case "text":
			if text, _ := b["text"].(string); text != "" {
				out = append(out, csStyleText.Render(text))
			}
		case "tool_use":
			name, _ := b["name"].(string)
			id, _ := b["id"].(string)
			if id != "" && name != "" {
				c.toolNames[id] = name
			}
			input, _ := b["input"].(map[string]any)
			out = append(out, csArrowOut+" "+csStyleTool.Render(name)+summariseInput(name, input))
		case "thinking":
			if text, _ := b["thinking"].(string); text != "" {
				out = append(out, csStyleThink.Render("  (thinking) "+truncateInline(text, 200)))
			}
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

func (c *ClaudeStream) formatUser(ev map[string]any) (string, bool) {
	msg, _ := ev["message"].(map[string]any)
	if msg == nil {
		return "", false
	}
	content, _ := msg["content"].([]any)
	var out []string
	for _, block := range content {
		b, _ := block.(map[string]any)
		if b == nil {
			continue
		}
		if bt, _ := b["type"].(string); bt != "tool_result" {
			continue
		}
		id, _ := b["tool_use_id"].(string)
		name := c.toolNames[id]
		if name == "" {
			name = "tool"
		}
		isErr, _ := b["is_error"].(bool)
		marker := csCheck
		if isErr {
			marker = csCross
		}
		body := truncateInline(contentToString(b["content"]), 120)
		out = append(out, csArrowIn+" "+marker+" "+csStyleTool.Render(name)+csStyleMuted.Render(": ")+csStyleResult.Render(body))
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

func summariseInput(toolName string, input map[string]any) string {
	if input == nil {
		return ""
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, _ := input[k].(string); v != "" {
				return v
			}
		}
		return ""
	}
	sep := csStyleMuted.Render(": ")
	switch toolName {
	case "Bash":
		if v := pick("command"); v != "" {
			return sep + csStyleCmd.Render(truncateInline(v, 100))
		}
	case "Read", "Write", "Edit", "NotebookEdit":
		if v := pick("file_path", "notebook_path"); v != "" {
			return sep + csStylePath.Render(v)
		}
	case "Glob":
		if v := pick("pattern"); v != "" {
			return sep + csStylePath.Render(v)
		}
	case "Grep":
		if v := pick("pattern"); v != "" {
			return sep + csStyleCmd.Render(truncateInline(v, 80))
		}
	case "WebFetch":
		if v := pick("url"); v != "" {
			return sep + csStylePath.Render(v)
		}
	}
	return ""
}

func contentToString(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "text" {
				if s, _ := m["text"].(string); s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func truncateInline(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
