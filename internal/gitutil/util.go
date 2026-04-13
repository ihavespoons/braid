// Package gitutil wraps the git CLI for braid's git-dependent features:
// worktree management for compositions and repository sanity checks.
package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// run executes git with args in cwd, returning stdout (trimmed) or an error
// that includes stderr for diagnostics.
func run(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsGitRepo reports whether cwd is inside a git working tree.
func IsGitRepo(cwd string) bool {
	_, err := run(cwd, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// HasHead reports whether the repository has at least one commit.
func HasHead(cwd string) bool {
	_, err := run(cwd, "rev-parse", "HEAD")
	return err == nil
}

// EnsureHead creates an empty initial commit if the repo has no HEAD.
// Compositions require a HEAD so `git worktree add` has a base to branch from.
func EnsureHead(cwd string) error {
	if HasHead(cwd) {
		return nil
	}
	_, err := run(cwd, "commit", "--allow-empty", "-m", "initial (braid)")
	return err
}

// CurrentHead returns the SHA of HEAD in cwd.
func CurrentHead(cwd string) (string, error) {
	return run(cwd, "rev-parse", "HEAD")
}

// IsWorkingTreeClean reports whether there are no uncommitted changes —
// neither in the index nor in the working tree.
func IsWorkingTreeClean(cwd string) (bool, error) {
	if _, err := run(cwd, "diff", "--quiet"); err != nil {
		// git diff --quiet exits 1 when there are changes; treat that as "dirty".
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, nil
		}
		return false, err
	}
	if _, err := run(cwd, "diff", "--cached", "--quiet"); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// FindProjectRoot walks upward from start until it finds a .git directory,
// returning the path to that directory's parent. Returns an error if no
// .git ancestor exists.
func FindProjectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (info.IsDir() || !info.IsDir()) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git repository found at or above %s", abs)
		}
		dir = parent
	}
}

// HasCommandOnPath reports whether name is found in PATH.
func HasCommandOnPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SessionID generates a short, unique identifier for a composition run.
// Format: YYYYMMDD-HHmmss.
func SessionID() string {
	return time.Now().Format("20060102-150405")
}
