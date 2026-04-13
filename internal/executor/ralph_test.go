package executor

import (
	"testing"

	"github.com/ihavespoons/braid/internal/ast"
)

func TestExecuteRalph_DoneFirstTask(t *testing.T) {
	mock := newMockRunner()
	mock.queue("work", "did task 1")
	mock.queue("ralph", "DONE — all tasks complete")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RalphNode{
		Inner:      &ast.WorkNode{Prompt: "work through the list"},
		MaxTasks:   5,
		GatePrompt: "are all tasks complete",
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != VerdictDone {
		t.Errorf("Verdict: got %v, want DONE", result.Verdict)
	}
	if result.Iterations != 1 {
		t.Errorf("Iterations: got %d, want 1", result.Iterations)
	}
	// 2 calls: work + ralph gate.
	if len(mock.calls) != 2 {
		t.Errorf("calls: got %d, want 2", len(mock.calls))
	}
}

func TestExecuteRalph_NextThenDone(t *testing.T) {
	mock := newMockRunner()
	// Task 1: work → ralph(NEXT)
	mock.queue("work", "did task 1")
	mock.queue("ralph", "NEXT — more to do")
	// Task 2: work → ralph(DONE)
	mock.queue("work", "did task 2")
	mock.queue("ralph", "DONE — finished")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RalphNode{
		Inner:      &ast.WorkNode{Prompt: "process tasks"},
		MaxTasks:   5,
		GatePrompt: "are we done",
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != VerdictDone {
		t.Errorf("Verdict: got %v, want DONE", result.Verdict)
	}
	if result.Iterations != 2 {
		t.Errorf("Iterations: got %d, want 2", result.Iterations)
	}
	// 4 calls: (work+ralph)*2
	if len(mock.calls) != 4 {
		t.Errorf("calls: got %d, want 4", len(mock.calls))
	}
}

func TestExecuteRalph_MaxTasksExhausted(t *testing.T) {
	mock := newMockRunner()
	// Every ralph gate says NEXT — loop exhausts MaxTasks.
	for range 3 {
		mock.queue("work", "did something")
		mock.queue("ralph", "NEXT — keep going")
	}

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RalphNode{
		Inner:      &ast.WorkNode{Prompt: "infinite tasks"},
		MaxTasks:   3,
		GatePrompt: "are we done",
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != VerdictMaxIterations {
		t.Errorf("Verdict: got %v, want MAX_ITERATIONS", result.Verdict)
	}
	if result.Iterations != 3 {
		t.Errorf("Iterations: got %d, want 3", result.Iterations)
	}
}

func TestExecuteRalph_InnerMaxIterationsShortCircuits(t *testing.T) {
	// Inner is a review loop that exhausts its iterations — ralph should
	// stop immediately without running its gate (we didn't queue a ralph
	// response, so this asserts ralph never tries to call it).
	mock := newMockRunner()
	for range 3 {
		mock.queue("work", "attempt")
		mock.queue("review", "issues")
		mock.queue("gate", "ITERATE")
	}
	// Second iteration's work→iterate transition:
	mock.queue("iterate", "attempt")
	mock.queue("review", "issues")
	mock.queue("gate", "ITERATE")
	mock.queue("iterate", "attempt")
	mock.queue("review", "issues")
	mock.queue("gate", "ITERATE")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RalphNode{
		Inner: &ast.ReviewNode{
			Inner:         &ast.WorkNode{Prompt: "hard task"},
			MaxIterations: 3,
		},
		MaxTasks:   5,
		GatePrompt: "done?",
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Inner verdict propagates — ralph surfaces MAX_ITERATIONS because the
	// inner loop couldn't converge on task 1.
	if result.Verdict != VerdictMaxIterations {
		t.Errorf("Verdict: got %v, want MAX_ITERATIONS (from inner)", result.Verdict)
	}
}
