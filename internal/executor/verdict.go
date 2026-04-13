package executor

import "strings"

// doneKeywords terminate a review loop with DONE.
var doneKeywords = []string{"DONE", "PASS", "COMPLETE", "APPROVE", "ACCEPT"}

// iterateKeywords continue a review loop with ITERATE.
var iterateKeywords = []string{"ITERATE", "REVISE", "RETRY"}

// ralphDoneKeywords end a ralph progression.
var ralphDoneKeywords = []string{"DONE", "COMPLETE", "FINISHED"}

// ralphNextKeywords advance to the next ralph task.
var ralphNextKeywords = []string{"NEXT", "CONTINUE"}

// ParseGateVerdict scans output line-by-line. The first line containing any
// doneKeyword returns VerdictDone; iterateKeyword returns VerdictIterate.
// Empty/ambiguous output defaults to VerdictIterate to favor progress safety.
func ParseGateVerdict(output string) Verdict {
	for line := range strings.SplitSeq(output, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		for _, kw := range doneKeywords {
			if strings.Contains(upper, kw) {
				return VerdictDone
			}
		}
		for _, kw := range iterateKeywords {
			if strings.Contains(upper, kw) {
				return VerdictIterate
			}
		}
	}
	return VerdictIterate
}

// RalphVerdict is the outcome of a ralph gate.
type RalphVerdict string

const (
	RalphVerdictDone RalphVerdict = "DONE"
	RalphVerdictNext RalphVerdict = "NEXT"
)

// ParseRalphVerdict scans output for NEXT/CONTINUE or DONE/COMPLETE/FINISHED.
// Defaults to DONE (fail-safe) to avoid runaway task progression on ambiguous
// output.
func ParseRalphVerdict(output string) RalphVerdict {
	for line := range strings.SplitSeq(output, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		for _, kw := range ralphDoneKeywords {
			if strings.Contains(upper, kw) {
				return RalphVerdictDone
			}
		}
		for _, kw := range ralphNextKeywords {
			if strings.Contains(upper, kw) {
				return RalphVerdictNext
			}
		}
	}
	return RalphVerdictDone
}
