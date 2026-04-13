package executor

import (
	"regexp"
	"strings"
)

// Verdict matchers require the keyword to stand alone at the start of a
// cleaned line (after stripping leading markdown punctuation), with a word
// boundary after it. This prevents substring matches like "completed" →
// COMPLETE or "continued" → CONTINUE, which were triggering the wrong
// verdict on ordinary prose.
var (
	gateDoneRe    = regexp.MustCompile(`^(DONE|PASS|COMPLETE|APPROVE|ACCEPT)\b`)
	gateIterateRe = regexp.MustCompile(`^(ITERATE|REVISE|RETRY)\b`)

	ralphDoneRe = regexp.MustCompile(`^(DONE|COMPLETE|FINISHED)\b`)
	ralphNextRe = regexp.MustCompile(`^(NEXT|CONTINUE)\b`)

	// Leading markdown/whitespace to strip before matching: `# `, `## `,
	// `**`, `> `, `- `, `: `, etc.
	leadingMarkup = regexp.MustCompile(`^[\s#*>\-:` + "`" + `]+`)
)

// cleanLine strips leading whitespace + common markdown markers and
// uppercases the result for verdict keyword matching.
func cleanLine(line string) string {
	line = leadingMarkup.ReplaceAllString(line, "")
	return strings.ToUpper(strings.TrimSpace(line))
}

// ParseGateVerdict scans output line-by-line for a verdict keyword
// standing alone at the start of a line. The last match wins so that an
// agent that explains its reasoning and then emits the verdict gets the
// expected result. Empty/ambiguous output defaults to VerdictIterate to
// favor progress safety (keep iterating rather than prematurely DONE).
func ParseGateVerdict(output string) Verdict {
	last := Verdict("")
	for _, line := range strings.Split(output, "\n") {
		cleaned := cleanLine(line)
		if gateIterateRe.MatchString(cleaned) {
			last = VerdictIterate
		} else if gateDoneRe.MatchString(cleaned) {
			last = VerdictDone
		}
	}
	if last == "" {
		return VerdictIterate
	}
	return last
}

// RalphVerdict is the outcome of a ralph gate.
type RalphVerdict string

const (
	RalphVerdictDone RalphVerdict = "DONE"
	RalphVerdictNext RalphVerdict = "NEXT"
)

// ParseRalphVerdict scans output for NEXT/CONTINUE or DONE/COMPLETE/FINISHED
// standing alone at the start of a line. Last match wins. Empty/ambiguous
// output defaults to DONE (fail-safe) to avoid runaway task progression.
func ParseRalphVerdict(output string) RalphVerdict {
	last := RalphVerdict("")
	for _, line := range strings.Split(output, "\n") {
		cleaned := cleanLine(line)
		if ralphNextRe.MatchString(cleaned) {
			last = RalphVerdictNext
		} else if ralphDoneRe.MatchString(cleaned) {
			last = RalphVerdictDone
		}
	}
	if last == "" {
		return RalphVerdictDone
	}
	return last
}
