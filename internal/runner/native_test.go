package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ihavespoons/braid/internal/config"
)

// runScript executes the NativeRunner with a bash script masquerading as the
// agent binary, by temporarily prepending a scratch directory to PATH. This
// lets us exercise the stdin/stdout/stderr plumbing without needing the
// real `claude` CLI on the test host.
func runScript(t *testing.T, scriptBody string, prompt string) (string, []string, error) {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	scratch := t.TempDir()
	scriptPath := filepath.Join(scratch, "claude")
	content := "#!/usr/bin/env bash\n" + scriptBody
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", scratch+string(os.PathListSeparator)+oldPath)

	r := NewNative(scratch, nil)

	var mu sync.Mutex
	var lines []string
	onLine := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, s)
	}

	out, err := r.RunAgent(context.Background(), Invocation{
		Agent:  "claude",
		Model:  "test-model",
		Prompt: prompt,
		OnLine: onLine,
	})
	return out, lines, err
}

func TestNativeRunner_StreamsLines(t *testing.T) {
	script := `
# Ignore the flags and read stdin, emit it back line-by-line with a prefix.
while IFS= read -r line; do
  printf '[echo] %s\n' "$line"
done
`
	out, lines, err := runScript(t, script, "alpha\nbeta\ngamma\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "[echo] alpha\n[echo] beta\n[echo] gamma\n"
	if out != want {
		t.Errorf("output: got %q, want %q", out, want)
	}
	if len(lines) != 3 {
		t.Errorf("lines: got %d, want 3: %v", len(lines), lines)
	}
	if lines[0] != "[echo] alpha" || lines[2] != "[echo] gamma" {
		t.Errorf("streamed lines incorrect: %v", lines)
	}
}

func TestNativeRunner_NonZeroExitReturnsError(t *testing.T) {
	script := `
echo "partial stdout"
echo "boom" >&2
exit 7
`
	_, _, err := runScript(t, script, "")
	if err == nil {
		t.Fatal("expected error from exit 7")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should include stderr: %v", err)
	}
}

func TestNativeRunner_ContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	scratch := t.TempDir()
	scriptPath := filepath.Join(scratch, "claude")
	// Slow script that would block for a long time without cancellation.
	content := "#!/usr/bin/env bash\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", scratch+string(os.PathListSeparator)+os.Getenv("PATH"))

	r := NewNative(scratch, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	go func() {
		_, _ = r.RunAgent(ctx, Invocation{Agent: "claude"})
		close(done)
	}()

	select {
	case <-done:
		// Good — context cancellation killed the subprocess.
	case <-time.After(5 * time.Second):
		t.Fatal("subprocess did not exit after context cancellation")
	}
}

func TestNativeRunner_StopTerminates(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	scratch := t.TempDir()
	scriptPath := filepath.Join(scratch, "claude")
	content := "#!/usr/bin/env bash\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", scratch+string(os.PathListSeparator)+os.Getenv("PATH"))

	r := NewNative(scratch, nil)
	done := make(chan struct{})
	go func() {
		_, _ = r.RunAgent(context.Background(), Invocation{Agent: "claude"})
		close(done)
	}()

	// Give the subprocess a moment to start.
	time.Sleep(50 * time.Millisecond)
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("subprocess did not exit after Stop")
	}
}

func TestPool_LazyGetAndReuse(t *testing.T) {
	created := 0
	p := NewPool(func(mode config.SandboxMode) (AgentRunner, error) {
		_ = mode
		created++
		return &noopRunner{}, nil
	})

	r1, err := p.Get(config.SandboxAgent)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := p.Get(config.SandboxAgent)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Error("pool should reuse same runner for same mode")
	}
	if created != 1 {
		t.Errorf("factory called %d times, want 1", created)
	}

	if _, err := p.Get(config.SandboxDocker); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Errorf("different mode should create new runner, got %d", created)
	}
}

func TestPool_StopAllPreventsReuse(t *testing.T) {
	p := NewPool(func(config.SandboxMode) (AgentRunner, error) {
		return &noopRunner{}, nil
	})
	_, _ = p.Get(config.SandboxAgent)
	if err := p.StopAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(config.SandboxAgent); err == nil {
		t.Error("Get after StopAll should fail")
	}
}

// noopRunner satisfies AgentRunner for pool tests.
type noopRunner struct{}

func (*noopRunner) RunAgent(context.Context, Invocation) (string, error) {
	return "", nil
}
func (*noopRunner) Stop() error { return nil }
