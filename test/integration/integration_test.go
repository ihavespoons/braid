// Package integration runs the real braid binary against a mock claude
// and a scratch git repo. These tests are the ground truth for braid's
// user-facing behavior — if unit tests pass but these break, something in
// the wiring between packages has drifted.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce  sync.Once
	binaryPath string
	buildErr   error
)

// buildBraid compiles the braid binary once per test process. All tests
// share the same build to keep runtimes under a few seconds.
func buildBraid(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			buildErr = err
			return
		}
		dir, err := os.MkdirTemp("", "braid-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binaryPath = filepath.Join(dir, "braid")
		if runtime.GOOS == "windows" {
			binaryPath += ".exe"
		}
		// Walk to the repo root — tests live at test/integration so
		// the module root is two directories up.
		_, thisFile, _, _ := runtime.Caller(0)
		repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

		cmd := exec.Command("go", "build", "-o", binaryPath, ".")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{err: err, output: string(out)}
		}
	})
	if buildErr != nil {
		t.Fatalf("building braid: %v", buildErr)
	}
	return binaryPath
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

// setupRepo creates a scratch git repo with an initial commit, a mock
// "claude" binary on PATH, and returns the repo dir. All scratch state is
// cleaned up via t.TempDir and t.Setenv.
func setupRepo(t *testing.T, mockClaude string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@braid.local"},
		{"config", "user.name", "braid-test"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Install the mock claude script into a scratch PATH dir so the braid
	// binary invokes it instead of the real claude.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "claude")
	if err := os.WriteFile(scriptPath, []byte(mockClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	// PATH must include system dirs so `bash` and `grep` inside the mock
	// script still resolve.
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return repo
}

// runBraid invokes the binary in cwd with args, forcing non-TUI output
// (BRAID_NO_TUI=1) so stderr is deterministic and grepable.
func runBraid(t *testing.T, cwd string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	bin := buildBraid(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "BRAID_NO_TUI=1")

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// mockClaudeReview responds to work → "did the work", review → "looks good",
// gate → "DONE". Matches the "Step: **X**" marker embedded by the template.
const mockClaudeReview = `#!/usr/bin/env bash
prompt=$(cat)
if echo "$prompt" | grep -q 'Step: \*\*work\*\*'; then
  echo "did the work"
elif echo "$prompt" | grep -q 'Step: \*\*review\*\*'; then
  echo "looks good"
elif echo "$prompt" | grep -q 'Step: \*\*gate\*\*'; then
  echo "DONE"
elif echo "$prompt" | grep -q 'Step: \*\*iterate\*\*'; then
  echo "iterated"
elif echo "$prompt" | grep -q 'Step: \*\*ralph\*\*'; then
  # Default ralph verdict: DONE (so tests that don't care finish quickly).
  echo "DONE"
else
  echo "(mock default)"
fi
`

func TestIntegration_Init(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, err := runBraid(t, repo, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, stderr)
	}
	for _, f := range []string{"config.json", "docker.json", ".gitignore"} {
		if _, serr := os.Stat(filepath.Join(repo, ".braid", f)); serr != nil {
			t.Errorf("init did not create %s: %v", f, serr)
		}
	}
}

func TestIntegration_WorkOnly(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, err := runBraid(t, repo, "do the task")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "did the work") {
		t.Errorf("expected work output, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Done") && !strings.Contains(stderr, "Completed") {
		t.Errorf("expected completion marker, got:\n%s", stderr)
	}
}

func TestIntegration_ReviewLoop(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, err := runBraid(t, repo, "fix bug", "review")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"did the work", "looks good", "DONE", "Completed in 1 iteration"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected %q in output:\n%s", want, stderr)
		}
	}
}

func TestIntegration_ReviewLoopIterates(t *testing.T) {
	// Gate returns ITERATE first time, DONE second time.
	mock := `#!/usr/bin/env bash
prompt=$(cat)
counter_file="$BRAID_TEST_COUNTER"
count=$(cat "$counter_file" 2>/dev/null || echo 0)
if echo "$prompt" | grep -q 'Step: \*\*gate\*\*'; then
  count=$((count+1))
  echo $count > "$counter_file"
  if [ "$count" = "1" ]; then echo "ITERATE"; else echo "DONE"; fi
elif echo "$prompt" | grep -q 'Step: \*\*review\*\*'; then
  echo "issues found"
elif echo "$prompt" | grep -q 'Step: \*\*iterate\*\*'; then
  echo "fixed it"
elif echo "$prompt" | grep -q 'Step: \*\*work\*\*'; then
  echo "initial attempt"
else
  echo "(mock default)"
fi
`
	repo := setupRepo(t, mock)
	counterFile := filepath.Join(repo, ".counter")
	t.Setenv("BRAID_TEST_COUNTER", counterFile)

	_, stderr, err := runBraid(t, repo, "fix bug", "review")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "Completed in 2 iteration") {
		t.Errorf("expected 2-iteration completion, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Iteration 2/3") {
		t.Errorf("expected second iteration marker, got:\n%s", stderr)
	}
}

func TestIntegration_Repeat(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, err := runBraid(t, repo, "task", "x3")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"Repeat pass 1/3", "Repeat pass 2/3", "Repeat pass 3/3"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected %q in output:\n%s", want, stderr)
		}
	}
}

func TestIntegration_Ralph(t *testing.T) {
	// Ralph gate returns NEXT then DONE.
	mock := `#!/usr/bin/env bash
prompt=$(cat)
counter_file="$BRAID_TEST_COUNTER"
count=$(cat "$counter_file" 2>/dev/null || echo 0)
if echo "$prompt" | grep -q 'Step: \*\*ralph\*\*'; then
  count=$((count+1))
  echo $count > "$counter_file"
  if [ "$count" = "1" ]; then echo "NEXT"; else echo "DONE"; fi
elif echo "$prompt" | grep -q 'Step: \*\*work\*\*'; then
  echo "did work"
else
  echo "(mock default)"
fi
`
	repo := setupRepo(t, mock)
	counterFile := filepath.Join(repo, ".ralph-counter")
	t.Setenv("BRAID_TEST_COUNTER", counterFile)

	_, stderr, err := runBraid(t, repo, "task", "ralph", "5", "are we done")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"Ralph task 1/5", "Ralph task 2/5", "ralph complete after 2"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected %q in output:\n%s", want, stderr)
		}
	}
	// Should NOT have started task 3.
	if strings.Contains(stderr, "Ralph task 3/5") {
		t.Errorf("ralph should have stopped at task 2 after DONE:\n%s", stderr)
	}
}

func TestIntegration_Composition_Pick(t *testing.T) {
	mock := `#!/usr/bin/env bash
prompt=$(cat)
if echo "$prompt" | grep -q 'Respond with exactly one line: PICK'; then
  echo "PICK 1"
elif echo "$prompt" | grep -q 'Step: \*\*work\*\*'; then
  echo "implementation"
else
  echo "(mock default)"
fi
`
	repo := setupRepo(t, mock)
	_, stderr, err := runBraid(t, repo, "approach A", "vs", "approach B", "pick", "best one")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, stderr)
	}
	for _, want := range []string{"Composition", "run 1: done", "run 2: done", "PICK 1", "picked run 1", "merged"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected %q in output:\n%s", want, stderr)
		}
	}
}

func TestIntegration_ParseError(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, err := runBraid(t, repo, "work", "repeat")
	if err == nil {
		t.Error("expected error for 'repeat' without count")
	}
	if !strings.Contains(stderr, "repeat requires a number") {
		t.Errorf("expected parse error message, got:\n%s", stderr)
	}
}

func TestIntegration_Doctor_ReportsStatus(t *testing.T) {
	repo := setupRepo(t, mockClaudeReview)
	_, stderr, _ := runBraid(t, repo, "doctor")
	// Doctor may exit 1 when docker isn't running — that's expected.
	// What matters: it reports the checks it ran.
	for _, want := range []string{"Braid Doctor", "git available", "claude CLI available"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("expected %q in doctor output:\n%s", want, stderr)
		}
	}
}
