package gitutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// Worktree describes one created git worktree, with the filesystem path and
// the branch name pointing at its HEAD.
type Worktree struct {
	Path   string
	Branch string
}

// CreateWorktree adds a new worktree at path rooted at a fresh branch
// branched from HEAD of repoRoot. The target path must not exist.
func CreateWorktree(repoRoot, path, branch string) (*Worktree, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating parent for worktree: %w", err)
	}
	if _, err := run(repoRoot, "worktree", "add", "-b", branch, path); err != nil {
		return nil, err
	}
	return &Worktree{Path: path, Branch: branch}, nil
}

// RemoveWorktree removes the worktree at path and deletes the branch.
// Best-effort: errors during removal are returned but the caller typically
// logs and moves on since leaked worktrees can be cleaned up later with
// `git worktree prune`.
func RemoveWorktree(repoRoot, path, branch string) error {
	// --force lets us remove even if there are uncommitted changes in the
	// worktree (rare, but we want cleanup to succeed).
	if _, err := run(repoRoot, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	if branch != "" {
		if _, err := run(repoRoot, "branch", "-D", branch); err != nil {
			return err
		}
	}
	return nil
}

// DiffAgainst returns the diff from base to HEAD in the given worktree.
// Returns empty string (not an error) when there's no diff.
func DiffAgainst(worktreePath, base string) (string, error) {
	out, err := run(worktreePath, "diff", base+"..HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

// Status returns the `git status --porcelain` output, useful for detecting
// whether a worktree has changes before committing.
func Status(cwd string) (string, error) {
	return run(cwd, "status", "--porcelain")
}

// AddAll stages all changes in cwd (`git add -A`).
func AddAll(cwd string) error {
	_, err := run(cwd, "add", "-A")
	return err
}

// Commit creates a commit with the given message. Returns nil if there's
// nothing to commit (empty status).
func Commit(cwd, message string) error {
	status, err := Status(cwd)
	if err != nil {
		return err
	}
	if status == "" {
		return nil
	}
	_, err = run(cwd, "commit", "-m", message)
	return err
}

// Merge runs `git merge <branch> --no-edit` in cwd.
func Merge(cwd, branch string) error {
	_, err := run(cwd, "merge", branch, "--no-edit")
	return err
}
