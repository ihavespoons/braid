package executor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/template"
)

// mockRunner returns canned responses keyed by step name (extracted from the
// rendered prompt's "Step: **X**" header). This lets tests exercise the
// executor without spawning real agents.
type mockRunner struct {
	mu        sync.Mutex
	responses map[string][]string // step name → queued responses
	calls     []mockCall
}

type mockCall struct {
	Agent  config.AgentName
	Model  string
	Prompt string
}

func newMockRunner() *mockRunner {
	return &mockRunner{responses: map[string][]string{}}
}

// queue enqueues a response for the given step. Multiple responses for the
// same step are consumed in FIFO order (one per iteration).
func (m *mockRunner) queue(step, response string) {
	m.responses[step] = append(m.responses[step], response)
}

func (m *mockRunner) RunAgent(_ context.Context, agent config.AgentName, model, prompt string, onLine func(string)) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Agent: agent, Model: model, Prompt: prompt})

	step := extractStep(prompt)
	if step == "" {
		step = "unknown"
	}
	queue := m.responses[step]
	if len(queue) == 0 {
		// Fall back to a generic default; tests that care supply explicit responses.
		return "", nil
	}
	response := queue[0]
	m.responses[step] = queue[1:]
	if onLine != nil {
		onLine(response)
	}
	return response, nil
}

func (*mockRunner) Stop() error { return nil }

// extractStep pulls the step name from the rendered BRAID.md template,
// which always includes `Step: **<name>**`.
func extractStep(prompt string) string {
	const marker = "Step: **"
	i := findIndex(prompt, marker)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(marker):]
	end := findIndex(rest, "**")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func findIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// newTestContext builds an ExecutionContext and associated pool/session
// wired to a mockRunner shared across all sandbox modes.
func newTestContext(t *testing.T, mock *mockRunner) (*ExecutionContext, *runner.Pool, *braidlog.Session, context.Context) {
	t.Helper()

	// Silence logs during tests.
	braidlog.EnableColor = false

	dir := t.TempDir()
	cfg := config.Default()
	flags := &ast.ParsedFlags{}
	pool := runner.NewPool(func(mode config.SandboxMode) (runner.AgentRunner, error) {
		return mock, nil
	})
	session, err := braidlog.NewSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	ec := &ExecutionContext{
		ProjectRoot: dir,
		Config:      &cfg,
		Flags:       flags,
		StepConfig:  ResolveStepConfig(&cfg, flags),
		BraidMD:     template.DefaultBraidMD,
	}
	return ec, pool, session, context.Background()
}

func TestExecute_Work(t *testing.T) {
	mock := newMockRunner()
	mock.queue("work", "hello from work")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	result, err := Execute(ctx, &ast.WorkNode{Prompt: "do the thing"}, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LastMessage != "hello from work" {
		t.Errorf("LastMessage: got %q", result.LastMessage)
	}
	if len(mock.calls) != 1 {
		t.Errorf("calls: got %d, want 1", len(mock.calls))
	}
	// Work prompt should appear in the rendered template.
	if !contains(mock.calls[0].Prompt, "do the thing") {
		t.Errorf("prompt missing work text: %s", mock.calls[0].Prompt)
	}
}

func TestExecute_Repeat(t *testing.T) {
	mock := newMockRunner()
	mock.queue("work", "pass1")
	mock.queue("work", "pass2")
	mock.queue("work", "pass3")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RepeatNode{
		Inner: &ast.WorkNode{Prompt: "do it"},
		Count: 3,
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LastMessage != "pass3" {
		t.Errorf("last pass should win, got %q", result.LastMessage)
	}
	if len(mock.calls) != 3 {
		t.Errorf("should call 3 times, got %d", len(mock.calls))
	}
}

func TestExecute_ReviewLoopDoneFirstIter(t *testing.T) {
	mock := newMockRunner()
	mock.queue("work", "initial implementation")
	mock.queue("review", "looks good, no issues")
	mock.queue("gate", "DONE — all checks pass")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.ReviewNode{
		Inner:         &ast.WorkNode{Prompt: "fix the bug"},
		MaxIterations: 3,
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
	if len(mock.calls) != 3 {
		t.Errorf("should call work+review+gate once each, got %d", len(mock.calls))
	}
}

func TestExecute_ReviewLoopIteratesThenDone(t *testing.T) {
	mock := newMockRunner()
	// Iteration 1: work → review → gate(ITERATE)
	mock.queue("work", "attempt 1")
	mock.queue("review", "issues found")
	mock.queue("gate", "ITERATE — fix the failing tests")
	// Iteration 2: iterate → review → gate(DONE)
	mock.queue("iterate", "attempt 2 with fixes")
	mock.queue("review", "better")
	mock.queue("gate", "DONE")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.ReviewNode{
		Inner:         &ast.WorkNode{Prompt: "fix the bug"},
		MaxIterations: 3,
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
	if len(mock.calls) != 6 {
		t.Errorf("calls: got %d, want 6 (work,review,gate + iterate,review,gate)", len(mock.calls))
	}
}

func TestExecute_ReviewLoopMaxIterations(t *testing.T) {
	mock := newMockRunner()
	// Every gate returns ITERATE — loop exhausts.
	for range 3 {
		mock.queue("work", "still wrong")
		mock.queue("iterate", "still wrong")
		mock.queue("review", "still issues")
		mock.queue("gate", "ITERATE")
	}

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.ReviewNode{
		Inner:         &ast.WorkNode{Prompt: "impossible task"},
		MaxIterations: 3,
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

func TestExecute_RepeatReviewSessionLogWritten(t *testing.T) {
	mock := newMockRunner()
	mock.queue("work", "w1")
	mock.queue("review", "r1")
	mock.queue("gate", "DONE")
	mock.queue("work", "w2")
	mock.queue("review", "r2")
	mock.queue("gate", "DONE")

	ec, pool, session, ctx := newTestContext(t, mock)
	defer pool.StopAll()

	node := &ast.RepeatNode{
		Count: 2,
		Inner: &ast.ReviewNode{
			Inner:         &ast.WorkNode{Prompt: "task"},
			MaxIterations: 3,
		},
	}
	result, err := Execute(ctx, node, ec, pool, session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != VerdictDone {
		t.Errorf("Verdict: got %v, want DONE", result.Verdict)
	}
	// Session log path should be under projectRoot/.braid/logs
	want := filepath.Join(ec.ProjectRoot, ".braid", "logs")
	if !contains(session.Path, want) {
		t.Errorf("session path %q should be under %q", session.Path, want)
	}
}

func contains(haystack, needle string) bool {
	return findIndex(haystack, needle) >= 0
}
