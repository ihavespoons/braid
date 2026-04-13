package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/gitutil"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/retry"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/template"
	"github.com/ihavespoons/braid/internal/tui"
)

// pickVerdictRe matches "PICK N" (case-insensitive) anywhere in the output.
var pickVerdictRe = regexp.MustCompile(`(?i)\bPICK\s+(\d+)\b`)

// buildJudgePrompt assembles the prompt sent to a judge agent. The judge is
// shown each run's final message plus the optional criteria and asked to
// reply with "PICK N".
func buildJudgePrompt(successful []*runResult, criteria string) string {
	var b strings.Builder
	b.WriteString("You are judging multiple parallel implementations of the same task.\n")
	b.WriteString("Read the session logs below and select the best run.\n\n")
	if criteria != "" {
		b.WriteString("Selection criteria:\n")
		b.WriteString(criteria)
		b.WriteString("\n\n")
	}
	b.WriteString("Evaluate on correctness, completeness, code quality, and gate pass.\n")
	b.WriteString("Respond with exactly one line: PICK N (where N is 1-")
	b.WriteString(strconv.Itoa(len(successful)))
	b.WriteString(")\n\n")

	for _, r := range successful {
		fmt.Fprintf(&b, "--- Run %d final output ---\n%s\n\n", r.Index, r.LastMessage)
		if r.LogFile != "" {
			if data, err := os.ReadFile(r.LogFile); err == nil {
				fmt.Fprintf(&b, "--- Run %d session log ---\n%s\n\n", r.Index, string(data))
			}
		}
	}
	return b.String()
}

// parsePickVerdict extracts the chosen run number from judge output. Returns
// 0 if no valid "PICK N" pattern is present or N is out of range.
func parsePickVerdict(output string, maxN int) int {
	matches := pickVerdictRe.FindStringSubmatch(output)
	if len(matches) < 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil || n < 1 || n > maxN {
		return 0
	}
	return n
}

// resolvePick runs a judge agent to select the best branch, then merges the
// chosen branch into the parent working tree and cleans up worktrees.
func resolvePick(ctx context.Context, node *ast.CompositionNode, ec *ExecutionContext, all, successful []*runResult, session string) (*ExecutionResult, error) {
	// If only one branch succeeded, skip the judge and auto-select it.
	if len(successful) == 1 {
		winner := successful[0]
		ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("only run %d succeeded — auto-selecting", winner.Index)})
		if err := applyWinnerAndCleanup(ec, all, winner, session); err != nil {
			return nil, err
		}
		return &ExecutionResult{LastMessage: winner.LastMessage}, nil
	}

	ec.emit(tui.PhaseEvent{Title: "Picking best run"})

	judgePrompt := buildJudgePrompt(successful, node.Criteria)
	judgeOutput, err := runJudgeAgent(ctx, ec, judgePrompt)
	if err != nil {
		cleanupAllWorktrees(ec, all, session)
		return nil, fmt.Errorf("pick judge failed: %w", err)
	}

	winnerIdx := parsePickVerdict(judgeOutput, len(all))
	if winnerIdx == 0 {
		ec.emit(tui.LogEvent{Level: "warn", Text: "pick did not return a clear verdict; preserving worktrees for manual selection"})
		for _, r := range successful {
			ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("  run %d: git diff HEAD...%s", r.Index, r.BranchName)})
		}
		return &ExecutionResult{LastMessage: judgeOutput}, nil
	}

	var winner *runResult
	for _, r := range all {
		if r.Index == winnerIdx && r.Status == "done" {
			winner = r
			break
		}
	}
	if winner == nil {
		return &ExecutionResult{LastMessage: judgeOutput}, fmt.Errorf("judge picked run %d but that run was not successful", winnerIdx)
	}

	ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("picked run %d", winner.Index)})
	if err := applyWinnerAndCleanup(ec, all, winner, session); err != nil {
		return nil, err
	}
	return &ExecutionResult{LastMessage: winner.LastMessage, LogFile: winner.LogFile}, nil
}

// resolveMerge spawns a synthesis agent loop in a fresh "merge" worktree
// that reads each branch's diff+log from MERGE_CONTEXT.md and produces a
// unified implementation.
func resolveMerge(ctx context.Context, node *ast.CompositionNode, ec *ExecutionContext, all, successful []*runResult, session, baseCommit string) (*ExecutionResult, error) {
	if len(successful) == 1 {
		winner := successful[0]
		ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("only run %d succeeded — using as merge result", winner.Index)})
		if err := applyWinnerAndCleanup(ec, all, winner, session); err != nil {
			return nil, err
		}
		return &ExecutionResult{LastMessage: winner.LastMessage}, nil
	}

	criteria := node.Criteria
	if criteria == "" {
		criteria = "Combine the best elements from each run."
	}

	ec.emit(tui.PhaseEvent{Title: "Merging runs"})

	// Create the merge worktree (separate branch so we can git-merge it back).
	mergePath := filepath.Join(ec.ProjectRoot, ".braid", "race", session, "merge")
	mergeBranch := fmt.Sprintf("braid-%s-merge", session)
	mergeWT, err := gitutil.CreateWorktree(ec.ProjectRoot, mergePath, mergeBranch)
	if err != nil {
		return nil, fmt.Errorf("creating merge worktree: %w", err)
	}

	// Collect each branch's diff + log and write MERGE_CONTEXT.md so the
	// agent can read it without needing the parent to pipe it.
	var context strings.Builder
	for _, r := range successful {
		diff, _ := gitutil.DiffAgainst(r.WorktreePath, baseCommit)
		if diff == "" {
			diff = "(no changes)"
		}
		logContent := "(no log)"
		if r.LogFile != "" {
			if data, rerr := os.ReadFile(r.LogFile); rerr == nil {
				logContent = string(data)
			}
		}
		fmt.Fprintf(&context, "--- Run %d Diff ---\n%s\n\n--- Run %d Log ---\n%s\n\n", r.Index, diff, r.Index, logContent)
	}
	mergeContext := fmt.Sprintf("# Merge Context\n\nSynthesize the best parts of multiple parallel runs.\n\n## Criteria\n%s\n\n## Run Results\n\n%s\n",
		criteria, context.String())
	if err := os.WriteFile(filepath.Join(mergeWT.Path, "MERGE_CONTEXT.md"), []byte(mergeContext), 0o644); err != nil {
		return nil, fmt.Errorf("writing MERGE_CONTEXT.md: %w", err)
	}

	// Run a review loop inside the merge worktree.
	mergeSession, err := braidlog.NewSession(mergeWT.Path)
	if err != nil {
		return nil, err
	}
	defer mergeSession.Close()

	dockerCfg, _ := config.LoadDocker(ec.ProjectRoot)
	mergePool := runner.NewPool(BranchRunnerFactory(mergeWT.Path, ec.Config.Env, dockerCfg))
	defer mergePool.StopAll()

	mergeBraidMD := ec.BraidMD
	if loaded, lerr := template.Load(mergeWT.Path); lerr == nil {
		mergeBraidMD = loaded
	}

	mergeEC := &ExecutionContext{
		ProjectRoot: mergeWT.Path,
		Config:      ec.Config,
		Flags:       ec.Flags,
		StepConfig:  ec.StepConfig,
		BraidMD:     mergeBraidMD,
		ShowRequest: ec.ShowRequest,
		Events:      ec.Events,
	}

	mergeWorkPrompt := fmt.Sprintf(
		"Synthesize the best parts of the provided runs. Read MERGE_CONTEXT.md for the run diffs and logs.\n\nCriteria: %s\n\nCombine the strongest elements from each run into a single coherent implementation.",
		criteria)

	mergeResult, err := runAgentLoop(ctx, agentLoopConfig{
		WorkPrompt:    mergeWorkPrompt,
		ReviewPrompt:  defaultReviewPrompt,
		GatePrompt:    defaultGatePrompt,
		MaxIterations: 3,
	}, mergeEC, mergePool, mergeSession)
	if err != nil {
		_ = gitutil.RemoveWorktree(ec.ProjectRoot, mergeWT.Path, mergeBranch)
		cleanupAllWorktrees(ec, all, session)
		return nil, fmt.Errorf("merge loop failed: %w", err)
	}

	// Clean up MERGE_CONTEXT.md before committing.
	_ = os.Remove(filepath.Join(mergeWT.Path, "MERGE_CONTEXT.md"))

	if err := gitutil.AddAll(mergeWT.Path); err != nil {
		ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("merge add failed: %v", err)})
	}
	if err := gitutil.Commit(mergeWT.Path, "braid merge"); err != nil {
		ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("merge commit failed: %v", err)})
	}

	// Merge back into the parent branch.
	if err := gitutil.Merge(ec.ProjectRoot, mergeBranch); err != nil {
		ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("git merge failed: %v — branches preserved for manual resolution", err)})
		return &ExecutionResult{LastMessage: mergeResult.LastMessage, LogFile: mergeResult.LogFile}, nil
	}

	// Success — clean everything up.
	_ = gitutil.RemoveWorktree(ec.ProjectRoot, mergeWT.Path, mergeBranch)
	cleanupAllWorktrees(ec, all, session)
	ec.emit(tui.LogEvent{Level: "info", Text: "merged synthesis into current branch"})
	return &ExecutionResult{LastMessage: mergeResult.LastMessage, LogFile: mergeResult.LogFile}, nil
}

// resolveCompare invokes an agent to produce a markdown comparison document
// that the user reads manually. Worktrees are preserved so the user can
// inspect branches before deciding.
func resolveCompare(ctx context.Context, _ *ast.CompositionNode, ec *ExecutionContext, all, successful []*runResult, session, baseCommit string) (*ExecutionResult, error) {
	ec.emit(tui.PhaseEvent{Title: "Comparing runs"})

	var b strings.Builder
	b.WriteString("You are comparing multiple parallel implementations.\n")
	b.WriteString("Review the run diffs and logs below and produce a structured comparison document in Markdown.\n\n")
	b.WriteString("For each run, summarize:\n")
	b.WriteString("- What approach was taken\n- Key strengths\n- Key weaknesses\n- Notable implementation details\n\n")
	b.WriteString("Then provide an overall recommendation with reasoning.\n\n")

	for _, r := range successful {
		diff, _ := gitutil.DiffAgainst(r.WorktreePath, baseCommit)
		if diff == "" {
			diff = "(no changes)"
		}
		logContent := "(no log)"
		if r.LogFile != "" {
			if data, rerr := os.ReadFile(r.LogFile); rerr == nil {
				logContent = string(data)
			}
		}
		fmt.Fprintf(&b, "--- Run %d Diff ---\n%s\n\n--- Run %d Log ---\n%s\n\n", r.Index, diff, r.Index, logContent)
	}

	compareOutput, err := runJudgeAgent(ctx, ec, b.String())
	if err != nil {
		cleanupAllWorktrees(ec, all, session)
		return nil, fmt.Errorf("compare failed: %w", err)
	}

	// Write the comparison doc to .braid/compare-SESSION.md
	braidDir := filepath.Join(ec.ProjectRoot, ".braid")
	if err := os.MkdirAll(braidDir, 0o755); err != nil {
		return nil, err
	}
	comparePath := filepath.Join(braidDir, fmt.Sprintf("compare-%s.md", session))
	if err := os.WriteFile(comparePath, []byte(compareOutput), 0o644); err != nil {
		return nil, fmt.Errorf("writing comparison doc: %w", err)
	}
	ec.emit(tui.LogEvent{Level: "info", Text: "comparison written to " + comparePath})
	ec.emit(tui.LogEvent{Level: "info", Text: "run worktrees preserved for inspection:"})
	for _, r := range successful {
		ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("  run %d: %s", r.Index, r.WorktreePath)})
	}

	return &ExecutionResult{LastMessage: compareOutput}, nil
}

// runJudgeAgent runs a single agent invocation using the gate step config.
// Used by pick (judge) and compare (document generator). Output is captured
// in full and streamed line-by-line to the TUI.
func runJudgeAgent(ctx context.Context, ec *ExecutionContext, prompt string) (string, error) {
	gateStep := ec.StepConfig[config.StepGate]

	dockerCfg, _ := config.LoadDocker(ec.ProjectRoot)
	pool := runner.NewPool(BranchRunnerFactory(ec.ProjectRoot, ec.Config.Env, dockerCfg))
	defer pool.StopAll()

	runnerForMode, err := pool.Get(gateStep.Sandbox)
	if err != nil {
		return "", err
	}

	onLine := func(line string) { ec.emit(tui.LineEvent{Text: line}) }

	return retry.Do(ctx, ec.Config.Retry, retry.Options{
		OnWaiting: func(info retry.WaitingInfo) {
			ec.emit(tui.WaitingEvent{NextRetryAt: info.NextRetryAt, Attempt: info.Attempt, Err: info.Err.Error()})
		},
		OnRetry: func(info retry.RetryInfo) {
			ec.emit(tui.RetryEvent{Attempt: info.Attempt})
		},
	}, func() (string, error) {
		return runnerForMode.RunAgent(ctx, gateStep.Agent, gateStep.Model, prompt, onLine)
	})
}

// applyWinnerAndCleanup merges the winner branch into the parent working
// tree and removes all worktrees. Non-winner work is kept in branches (not
// worktrees) until the user explicitly prunes them.
func applyWinnerAndCleanup(ec *ExecutionContext, all []*runResult, winner *runResult, session string) error {
	if err := gitutil.Merge(ec.ProjectRoot, winner.BranchName); err != nil {
		ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("git merge %s failed: %v — branches preserved for manual resolution", winner.BranchName, err)})
		return nil
	}
	ec.emit(tui.LogEvent{Level: "info", Text: "merged " + winner.BranchName + " into current branch"})
	cleanupAllWorktrees(ec, all, session)
	return nil
}

// cleanupAllWorktrees removes every composition worktree and deletes the
// per-session directory under .braid/race/.
func cleanupAllWorktrees(ec *ExecutionContext, all []*runResult, session string) {
	for _, r := range all {
		if r.WorktreePath != "" {
			_ = gitutil.RemoveWorktree(ec.ProjectRoot, r.WorktreePath, r.BranchName)
		}
	}
	sessionDir := filepath.Join(ec.ProjectRoot, ".braid", "race", session)
	_ = os.RemoveAll(sessionDir)

	// Remove .braid/race if empty.
	raceDir := filepath.Join(ec.ProjectRoot, ".braid", "race")
	if entries, err := os.ReadDir(raceDir); err == nil && len(entries) == 0 {
		_ = os.Remove(raceDir)
	}
}

