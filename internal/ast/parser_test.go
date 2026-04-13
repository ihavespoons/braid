package ast

import (
	"reflect"
	"testing"
)

func TestSeparateFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFlags  map[string]string
		wantPos    []string
	}{
		{
			name:      "no flags",
			args:      []string{"work", "review"},
			wantFlags: map[string]string{},
			wantPos:   []string{"work", "review"},
		},
		{
			name:      "value flag with space",
			args:      []string{"--agent", "claude", "work"},
			wantFlags: map[string]string{"--agent": "claude"},
			wantPos:   []string{"work"},
		},
		{
			name:      "value flag with equals",
			args:      []string{"--agent=claude", "work"},
			wantFlags: map[string]string{"--agent": "claude"},
			wantPos:   []string{"work"},
		},
		{
			name:      "boolean flag",
			args:      []string{"--no-wait", "work"},
			wantFlags: map[string]string{"--no-wait": "true"},
			wantPos:   []string{"work"},
		},
		{
			name:      "-y shorthand",
			args:      []string{"-y", "work"},
			wantFlags: map[string]string{"--yes": "true"},
			wantPos:   []string{"work"},
		},
		{
			name:      "-h shorthand",
			args:      []string{"-h"},
			wantFlags: map[string]string{"--help": "true"},
			wantPos:   []string{},
		},
		{
			name:      "hide-request",
			args:      []string{"--hide-request", "work"},
			wantFlags: map[string]string{"--hide-request": "true"},
			wantPos:   []string{"work"},
		},
		{
			name:      "mixed",
			args:      []string{"--agent", "claude", "-y", "work", "review", "x3"},
			wantFlags: map[string]string{"--agent": "claude", "--yes": "true"},
			wantPos:   []string{"work", "review", "x3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotFlags, gotPos := SeparateFlags(tc.args)
			if !reflect.DeepEqual(gotFlags, tc.wantFlags) {
				t.Errorf("flags: got %v, want %v", gotFlags, tc.wantFlags)
			}
			if !reflect.DeepEqual(gotPos, tc.wantPos) {
				t.Errorf("positional: got %v, want %v", gotPos, tc.wantPos)
			}
		})
	}
}

func TestParseWorkOnly(t *testing.T) {
	node, _, err := Parse([]string{"fix the bug"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w, ok := node.(*WorkNode)
	if !ok {
		t.Fatalf("expected *WorkNode, got %T", node)
	}
	if w.Prompt != "fix the bug" {
		t.Errorf("prompt: got %q, want %q", w.Prompt, "fix the bug")
	}
}

func TestParseExplicitReview(t *testing.T) {
	node, _, err := Parse([]string{"work", "review"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*ReviewNode)
	if !ok {
		t.Fatalf("expected *ReviewNode, got %T", node)
	}
	if r.MaxIterations != 3 {
		t.Errorf("MaxIterations: got %d, want 3", r.MaxIterations)
	}
	if _, ok := r.Inner.(*WorkNode); !ok {
		t.Errorf("Inner: got %T, want *WorkNode", r.Inner)
	}
}

func TestParseReviewWithPrompts(t *testing.T) {
	node, _, err := Parse([]string{"work", "review", "review prompt", "gate prompt", "iterate prompt", "5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*ReviewNode)
	if !ok {
		t.Fatalf("expected *ReviewNode, got %T", node)
	}
	if r.ReviewPrompt != "review prompt" {
		t.Errorf("ReviewPrompt: got %q", r.ReviewPrompt)
	}
	if r.GatePrompt != "gate prompt" {
		t.Errorf("GatePrompt: got %q", r.GatePrompt)
	}
	if r.IteratePrompt != "iterate prompt" {
		t.Errorf("IteratePrompt: got %q", r.IteratePrompt)
	}
	if r.MaxIterations != 5 {
		t.Errorf("MaxIterations: got %d, want 5", r.MaxIterations)
	}
}

func TestParseImplicitReview(t *testing.T) {
	// "work" "review prompt" (no explicit "review" keyword) = implicit review
	node, _, err := Parse([]string{"work", "review prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*ReviewNode)
	if !ok {
		t.Fatalf("expected *ReviewNode, got %T", node)
	}
	if r.ReviewPrompt != "review prompt" {
		t.Errorf("ReviewPrompt: got %q", r.ReviewPrompt)
	}
}

func TestParseXN(t *testing.T) {
	node, _, err := Parse([]string{"work", "x3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*RepeatNode)
	if !ok {
		t.Fatalf("expected *RepeatNode, got %T", node)
	}
	if r.Count != 3 {
		t.Errorf("Count: got %d, want 3", r.Count)
	}
}

func TestParseRepeatN(t *testing.T) {
	node, _, err := Parse([]string{"work", "repeat", "5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*RepeatNode)
	if !ok {
		t.Fatalf("expected *RepeatNode, got %T", node)
	}
	if r.Count != 5 {
		t.Errorf("Count: got %d, want 5", r.Count)
	}
}

func TestParseReviewXN(t *testing.T) {
	node, _, err := Parse([]string{"work", "review", "x3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rep, ok := node.(*RepeatNode)
	if !ok {
		t.Fatalf("expected *RepeatNode, got %T", node)
	}
	if rep.Count != 3 {
		t.Errorf("Count: got %d", rep.Count)
	}
	if _, ok := rep.Inner.(*ReviewNode); !ok {
		t.Errorf("Inner: got %T, want *ReviewNode", rep.Inner)
	}
}

func TestParseRalph(t *testing.T) {
	node, _, err := Parse([]string{"work", "ralph", "5", "done when all tasks complete"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*RalphNode)
	if !ok {
		t.Fatalf("expected *RalphNode, got %T", node)
	}
	if r.MaxTasks != 5 {
		t.Errorf("MaxTasks: got %d", r.MaxTasks)
	}
	if r.GatePrompt != "done when all tasks complete" {
		t.Errorf("GatePrompt: got %q", r.GatePrompt)
	}
}

func TestParseRalphDefaultTasks(t *testing.T) {
	node, _, err := Parse([]string{"work", "ralph", "done when complete"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := node.(*RalphNode)
	if !ok {
		t.Fatalf("expected *RalphNode, got %T", node)
	}
	if r.MaxTasks != 100 {
		t.Errorf("MaxTasks: got %d, want 100", r.MaxTasks)
	}
}

func TestParseVN(t *testing.T) {
	node, _, err := Parse([]string{"work", "v3", "pick", "best implementation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := node.(*CompositionNode)
	if !ok {
		t.Fatalf("expected *CompositionNode, got %T", node)
	}
	if len(c.Branches) != 3 {
		t.Errorf("Branches: got %d, want 3", len(c.Branches))
	}
	if c.Resolver != ResolverPick {
		t.Errorf("Resolver: got %v, want Pick", c.Resolver)
	}
	if c.Criteria != "best implementation" {
		t.Errorf("Criteria: got %q", c.Criteria)
	}
}

func TestParseRaceN(t *testing.T) {
	node, _, err := Parse([]string{"work", "race", "3", "merge", "combined result"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := node.(*CompositionNode)
	if !ok {
		t.Fatalf("expected *CompositionNode, got %T", node)
	}
	if c.Resolver != ResolverMerge {
		t.Errorf("Resolver: got %v, want Merge", c.Resolver)
	}
}

func TestParseVs(t *testing.T) {
	node, _, err := Parse([]string{"approach A", "vs", "approach B", "pick", "most robust"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := node.(*CompositionNode)
	if !ok {
		t.Fatalf("expected *CompositionNode, got %T", node)
	}
	if len(c.Branches) != 2 {
		t.Errorf("Branches: got %d, want 2", len(c.Branches))
	}
	if c.Criteria != "most robust" {
		t.Errorf("Criteria: got %q", c.Criteria)
	}
}

func TestParseVsCompare(t *testing.T) {
	node, _, err := Parse([]string{"A", "vs", "B", "compare"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := node.(*CompositionNode)
	if !ok {
		t.Fatalf("expected *CompositionNode, got %T", node)
	}
	if c.Resolver != ResolverCompare {
		t.Errorf("Resolver: got %v, want Compare", c.Resolver)
	}
	if c.Criteria != "" {
		t.Errorf("compare should not have criteria, got %q", c.Criteria)
	}
}

func TestParseVsImplicitPick(t *testing.T) {
	// "A" vs "B" "criteria" — no explicit resolver, implicit pick with criteria
	node, _, err := Parse([]string{"A", "vs", "B", "criteria text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, ok := node.(*CompositionNode)
	if !ok {
		t.Fatalf("expected *CompositionNode, got %T", node)
	}
	// "criteria text" becomes part of the second branch's implicit review,
	// not a resolver criterion, because resolver keyword splitting requires pick/merge/compare.
	if len(c.Branches) != 2 {
		t.Errorf("Branches: got %d", len(c.Branches))
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty", []string{}},
		{"keyword as work", []string{"review"}},
		{"repeat without number", []string{"work", "repeat"}},
		{"race without number", []string{"work", "race"}},
		{"ralph without gate prompt", []string{"work", "ralph"}},
		{"ralph with only N", []string{"work", "ralph", "5"}},
		{"empty branch before vs", []string{"vs", "B"}},
		{"vs with only one branch", []string{"A", "vs"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Parse(tc.args); err == nil {
				t.Errorf("expected error for args %v", tc.args)
			}
		})
	}
}

func TestParseFlagsMaxIterations(t *testing.T) {
	_, flags, err := Parse([]string{"--max-iterations", "10", "work", "review"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.MaxIterations == nil || *flags.MaxIterations != 10 {
		t.Errorf("MaxIterations: got %v, want 10", flags.MaxIterations)
	}
}

func TestParseWorkFromFlag(t *testing.T) {
	node, _, err := Parse([]string{"--work", "fix the bug", "--review", "check quality"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --work + --review should produce a review node
	r, ok := node.(*ReviewNode)
	if !ok {
		t.Fatalf("expected *ReviewNode, got %T", node)
	}
	w, ok := r.Inner.(*WorkNode)
	if !ok {
		t.Fatalf("expected inner *WorkNode, got %T", r.Inner)
	}
	if w.Prompt != "fix the bug" {
		t.Errorf("Prompt: got %q", w.Prompt)
	}
}

func TestCloneNodeIndependent(t *testing.T) {
	original := &ReviewNode{
		Inner:         &WorkNode{Prompt: "original"},
		MaxIterations: 3,
	}
	clone := cloneNode(original).(*ReviewNode)

	// Mutating clone must not affect original
	clone.MaxIterations = 99
	clone.Inner.(*WorkNode).Prompt = "mutated"

	if original.MaxIterations != 3 {
		t.Errorf("original mutated: MaxIterations = %d", original.MaxIterations)
	}
	if original.Inner.(*WorkNode).Prompt != "original" {
		t.Errorf("original.Inner mutated")
	}
}
