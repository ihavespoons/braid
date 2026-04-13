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
		{"pass keyword", "the review PASS", VerdictDone},
		{"complete keyword", "Looks COMPLETE to me", VerdictDone},
		{"approve keyword", "I APPROVE this change", VerdictDone},
		{"accept keyword", "ACCEPT the work", VerdictDone},
		{"mixed case done", "done", VerdictDone},
		{"iterate alone", "ITERATE", VerdictIterate},
		{"revise keyword", "please REVISE the code", VerdictIterate},
		{"retry keyword", "RETRY with fixes", VerdictIterate},
		{"done wins over iterate when on earlier line", "DONE\nITERATE", VerdictDone},
		{"iterate wins when on earlier line", "some commentary\nITERATE here\nDONE later", VerdictIterate},
		{"empty defaults to iterate", "", VerdictIterate},
		{"no keywords defaults to iterate", "just some commentary", VerdictIterate},
		{"done embedded in word DOES NOT match (substring match used — so 'DONENESS' counts)", "the DONENESS of it", VerdictDone},
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
		{"next", "NEXT task", RalphVerdictNext},
		{"continue", "CONTINUE with the next item", RalphVerdictNext},
		{"done", "DONE — no more tasks", RalphVerdictDone},
		{"complete", "all tasks COMPLETE", RalphVerdictDone},
		{"finished", "FINISHED the list", RalphVerdictDone},
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
