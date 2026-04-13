package executor

import "testing"

func TestParseGateVerdict(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   Verdict
	}{
		{"done alone", "DONE", VerdictDone},
		{"done with reason", "DONE — all tests pass", VerdictDone},
		{"done with colon", "DONE: ready to merge", VerdictDone},
		{"done lowercase", "done", VerdictDone},
		{"markdown heading done", "## DONE", VerdictDone},
		{"bold done", "**DONE**", VerdictDone},
		{"quote done", "> DONE", VerdictDone},
		{"iterate alone", "ITERATE", VerdictIterate},
		{"iterate with reason", "ITERATE: more tests needed", VerdictIterate},
		{"last-match wins: done then iterate", "DONE\nITERATE", VerdictIterate},
		{"last-match wins: iterate then done", "some commentary\nITERATE here\nDONE", VerdictDone},
		{"reasoning followed by verdict", "The tests pass and code looks good.\n\nDONE", VerdictDone},
		{"substring in prose does NOT match", "I've completed a thorough review of the code", VerdictIterate},
		{"'complete' mid-sentence does NOT match", "all tasks are complete and tests pass", VerdictIterate},
		{"empty defaults to iterate", "", VerdictIterate},
		{"no keywords defaults to iterate", "just some commentary", VerdictIterate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGateVerdict(tc.output)
			if got != tc.want {
				t.Errorf("ParseGateVerdict(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestParseRalphVerdict(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   RalphVerdict
	}{
		{"next alone", "NEXT", RalphVerdictNext},
		{"next with reason", "NEXT — more tasks remain", RalphVerdictNext},
		{"continue", "CONTINUE", RalphVerdictNext},
		{"done alone", "DONE", RalphVerdictDone},
		{"markdown heading next", "## NEXT", RalphVerdictNext},
		{"reasoning then verdict", "Reviewed all changes.\n\nDONE", RalphVerdictDone},
		{"last-match wins: next then done", "NEXT\nDONE", RalphVerdictDone},
		{"last-match wins: done then next", "## DONE\nNEXT", RalphVerdictNext},
		{"'completed' in prose does NOT match", "I've completed a thorough review of all changes", RalphVerdictDone},
		{"'complete' mid-sentence does NOT match", "all tasks are complete", RalphVerdictDone},
		{"'continued' substring does NOT match", "work continued as planned", RalphVerdictDone},
		{"empty defaults to done", "", RalphVerdictDone},
		{"ambiguous defaults to done", "some commentary", RalphVerdictDone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRalphVerdict(tc.output)
			if got != tc.want {
				t.Errorf("ParseRalphVerdict(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}
