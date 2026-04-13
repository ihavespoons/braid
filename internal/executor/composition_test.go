package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/gitutil"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/template"
)

// newCompositionTestRepo creates a fresh git repo with one initial commit,
// ready for the executor to create worktrees in.
func newCompositionTestRepo(t *testing.T) string {
	t.Helper()
	if !gitutil.HasCommandOnPath("git") {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@braid.local"},
		{"config", "user.name", "braid-test"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// newCompositionContext builds an ExecutionContext for composition tests.
// Branches get their runner from runner.NewNative, which invokes whatever
// "claude" is on PATH — we install a mock via setupMockClaudeOnPath.
func newCompositionContext(t *testing.T, projectRoot string) *ExecutionContext {
	t.Helper()
	braidlog.EnableColor = false
	cfg := config.Default()
	flags := &ast.ParsedFlags{Yes: true}
	return &ExecutionContext{
		ProjectRoot: projectRoot,
		Config:      &cfg,
		Flags:       flags,
		StepConfig:  ResolveStepConfig(&cfg, flags),
		BraidMD:     template.DefaultBraidMD,
	}
}

// setupMockClaudeOnPath installs a bash script at $PATH/claude that
// responds to prompts based on marker substrings. Each (marker, response)
// entry is tried in order; the first matching marker wins.
//
// Markers match anywhere in the prompt — tests use template markers like
// "Step: **work**" for templated steps, or literal strings like
// "PICK N" / "Comparing" for out-of-band judge prompts.
type mockCase struct {
	Marker   string
	Response string
}

func setupMockClaudeOnPath(t *testing.T, cases []mockCase) {
	t.Helper()
	scratch := t.TempDir()
	scriptPath := filepath.Join(scratch, "claude")

	body := `#!/usr/bin/env bash
prompt=$(cat)
`
	for _, c := range cases {
		// Escape single quotes in the marker for grep.
		safeMarker := c.Marker
		body += "if echo \"$prompt\" | grep -q -- '" + safeMarker + "'; then\n"
		body += "  cat <<'MOCK_EOF'\n"
		body += c.Response + "\n"
		body += "MOCK_EOF\n"
		body += "  exit 0\n"
		body += "fi\n"
	}
	body += "echo \"(mock default)\"\n"
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", scratch+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCompositionPick_ChoosesWinner(t *testing.T) {
	repo := newCompositionTestRepo(t)

	// Match order matters: the judge prompt contains "PICK N" literal text
	// that we can key on; branch work goes through the template with
	// "Step: **work**" marker.
	setupMockClaudeOnPath(t, []mockCase{
		{Marker: "Respond with exactly one line: PICK", Response: "PICK 2"},
		{Marker: "Step: \\*\\*work\\*\\*", Response: "did the work"},
	})

	ec := newCompositionContext(t, repo)
	pool := runner.NewPool(func(mode config.SandboxMode) (runner.AgentRunner, error) {
		return runner.NewNative(repo, nil), nil
	})
	defer pool.StopAll()

	session, err := braidlog.NewSession(repo)
	if err != nil {
		t.Fatal(err)
	}

	node := &ast.CompositionNode{
		Branches: []ast.Node{
			&ast.WorkNode{Prompt: "approach A"},
			&ast.WorkNode{Prompt: "approach B"},
		},
		Resolver: ast.ResolverPick,
		Criteria: "most correct",
	}

	result, err := Execute(context.Background(), node, ec, pool, session)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.LastMessage == "" {
		t.Error("expected non-empty last message")
	}

	// After successful pick, worktrees should be cleaned up.
	raceDir := filepath.Join(repo, ".braid", "race")
	if _, err := os.Stat(raceDir); !os.IsNotExist(err) {
		// Best-effort check — some git versions leave stale metadata.
		entries, _ := os.ReadDir(raceDir)
		if len(entries) > 0 {
			t.Errorf("race dir should be cleaned up, still has %d entries", len(entries))
		}
	}
}

func TestCompositionCompare_WritesDoc(t *testing.T) {
	repo := newCompositionTestRepo(t)

	setupMockClaudeOnPath(t, []mockCase{
		{Marker: "produce a structured comparison document", Response: "# Comparison\n\nRun 1 is better."},
		{Marker: "Step: \\*\\*work\\*\\*", Response: "did the work"},
	})

	ec := newCompositionContext(t, repo)
	pool := runner.NewPool(func(mode config.SandboxMode) (runner.AgentRunner, error) {
		return runner.NewNative(repo, nil), nil
	})
	defer pool.StopAll()

	session, err := braidlog.NewSession(repo)
	if err != nil {
		t.Fatal(err)
	}

	node := &ast.CompositionNode{
		Branches: []ast.Node{
			&ast.WorkNode{Prompt: "approach A"},
			&ast.WorkNode{Prompt: "approach B"},
		},
		Resolver: ast.ResolverCompare,
	}

	result, err := Execute(context.Background(), node, ec, pool, session)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Expect a compare-SESSION.md file in .braid/
	entries, err := os.ReadDir(filepath.Join(repo, ".braid"))
	if err != nil {
		t.Fatal(err)
	}
	foundCompare := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" && len(e.Name()) > 8 && e.Name()[:8] == "compare-" {
			foundCompare = true
			break
		}
	}
	if !foundCompare {
		t.Errorf("expected a compare-*.md file in .braid/, got %v", entries)
	}

	if result.LastMessage == "" {
		t.Error("compare should return the comparison doc as lastMessage")
	}
}

func TestCompositionRequiresGitRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	ec := newCompositionContext(t, dir)
	pool := runner.NewPool(func(mode config.SandboxMode) (runner.AgentRunner, error) {
		return newMockRunner(), nil
	})
	defer pool.StopAll()
	session, err := braidlog.NewSession(dir)
	if err != nil {
		t.Fatal(err)
	}

	node := &ast.CompositionNode{
		Branches: []ast.Node{
			&ast.WorkNode{Prompt: "a"},
			&ast.WorkNode{Prompt: "b"},
		},
		Resolver: ast.ResolverPick,
	}

	_, err = Execute(context.Background(), node, ec, pool, session)
	if err == nil {
		t.Error("expected error when not in a git repo")
	}
}

func TestParsePickVerdict(t *testing.T) {
	cases := []struct {
		output string
		maxN   int
		want   int
	}{
		{"PICK 2", 3, 2},
		{"pick 1", 3, 1},
		{"the judge says PICK 3 wins", 3, 3},
		{"PICK 5", 3, 0}, // out of range
		{"PICK 0", 3, 0}, // out of range
		{"no verdict here", 3, 0},
		{"", 3, 0},
	}
	for _, tc := range cases {
		got := parsePickVerdict(tc.output, tc.maxN)
		if got != tc.want {
			t.Errorf("parsePickVerdict(%q, %d) = %d, want %d", tc.output, tc.maxN, got, tc.want)
		}
	}
}
