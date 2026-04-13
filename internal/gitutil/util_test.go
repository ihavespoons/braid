package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestRepo creates a fresh git repo in a temp dir with a single initial
// commit. Tests that manipulate git state should use this helper.
func newTestRepo(t *testing.T) string {
	t.Helper()

	if !HasCommandOnPath("git") {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if _, err := run(dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	// Tests need a committer identity even for --allow-empty.
	if _, err := run(dir, "config", "user.email", "test@braid.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "config", "user.name", "braid-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "commit", "--allow-empty", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestIsGitRepoAndHasHead(t *testing.T) {
	if !HasCommandOnPath("git") {
		t.Skip("git not available")
	}

	nonRepo := t.TempDir()
	if IsGitRepo(nonRepo) {
		t.Error("non-repo should return false")
	}

	repo := newTestRepo(t)
	if !IsGitRepo(repo) {
		t.Error("repo should return true")
	}
	if !HasHead(repo) {
		t.Error("repo should have HEAD after initial commit")
	}
}

func TestEnsureHead_CreatesEmptyCommit(t *testing.T) {
	if !HasCommandOnPath("git") {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if _, err := run(dir, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	_, _ = run(dir, "config", "user.email", "test@braid.local")
	_, _ = run(dir, "config", "user.name", "braid-test")

	if HasHead(dir) {
		t.Fatal("fresh init should not have HEAD")
	}
	if err := EnsureHead(dir); err != nil {
		t.Fatal(err)
	}
	if !HasHead(dir) {
		t.Error("EnsureHead should create a commit")
	}
}

func TestIsWorkingTreeClean(t *testing.T) {
	dir := newTestRepo(t)

	clean, err := IsWorkingTreeClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("fresh repo should be clean")
	}

	// Create an untracked file. "clean" in braid's sense (uncommitted to
	// tracked files) stays true with untracked files only.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = IsWorkingTreeClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("untracked files should not make tree dirty in braid's sense")
	}

	// Modify a tracked file.
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = AddAll(dir)
	_ = Commit(dir, "add tracked")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = IsWorkingTreeClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Error("modified tracked file should make tree dirty")
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repo := newTestRepo(t)
	wtPath := filepath.Join(repo, "worktrees", "run-1")
	branch := "braid-test-run-1"

	wt, err := CreateWorktree(repo, wtPath, branch)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if wt.Path != wtPath {
		t.Errorf("Path: got %q, want %q", wt.Path, wtPath)
	}

	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree path should exist: %v", err)
	}

	// Make a change in the worktree and commit it; RemoveWorktree should
	// still succeed because we pass --force.
	if err := os.WriteFile(filepath.Join(wtPath, "x.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(repo, wtPath, branch); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree path should be removed, stat err = %v", err)
	}
}

func TestFindProjectRoot(t *testing.T) {
	repo := newTestRepo(t)
	sub := filepath.Join(repo, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	// On macOS, /tmp resolves via /private; both should agree after symlink eval.
	gotAbs, _ := filepath.EvalSymlinks(got)
	wantAbs, _ := filepath.EvalSymlinks(repo)
	if gotAbs != wantAbs {
		t.Errorf("got %q, want %q", gotAbs, wantAbs)
	}

	nonRepo := t.TempDir()
	if _, err := FindProjectRoot(nonRepo); err == nil {
		t.Error("expected error for non-repo directory")
	}
}

func TestSessionID_Format(t *testing.T) {
	id := SessionID()
	if len(id) != len("20060102-150405") {
		t.Errorf("SessionID format: got %q", id)
	}
	if id[8] != '-' {
		t.Errorf("SessionID separator at position 8: got %q", id)
	}
}
