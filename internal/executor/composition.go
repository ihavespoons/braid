package executor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/gitutil"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/sandbox"
	"github.com/ihavespoons/braid/internal/template"
	"github.com/ihavespoons/braid/internal/tui"
)

// BranchRunnerFactory returns a factory usable for a worktree-scoped pool.
// Shared here so composition and the resolvers build pools the same way.
func BranchRunnerFactory(projectRoot string, envPassthrough []string, dockerCfg config.DockerConfig) runner.Factory {
	return func(mode config.SandboxMode) (runner.AgentRunner, error) {
		switch mode {
		case config.SandboxDocker:
			return sandbox.NewDockerRunner(projectRoot, envPassthrough, dockerCfg), nil
		default:
			return runner.NewNative(projectRoot, envPassthrough), nil
		}
	}
}

// runResult captures the settled outcome of one composition branch.
type runResult struct {
	Index        int
	Status       string // "done" | "error" | "cancelled"
	LogFile      string
	WorktreePath string
	BranchName   string
	LastMessage  string
	Err          error
}

// executeComposition runs each branch in an isolated git worktree and then
// dispatches to the configured resolver (pick / merge / compare).
func executeComposition(ctx context.Context, node *ast.CompositionNode, ec *ExecutionContext, _ *runner.Pool, _ *braidlog.Session) (*ExecutionResult, error) {
	if !gitutil.IsGitRepo(ec.ProjectRoot) {
		return nil, errors.New("composition (vs / race / vN) requires a git repository — run `git init` in this directory first")
	}
	if err := gitutil.EnsureHead(ec.ProjectRoot); err != nil {
		return nil, fmt.Errorf("ensuring HEAD: %w", err)
	}
	clean, err := gitutil.IsWorkingTreeClean(ec.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("checking working tree: %w", err)
	}
	if !clean {
		return nil, errors.New("cannot run composition: working tree has uncommitted changes; please commit or stash first")
	}

	baseCommit, err := gitutil.CurrentHead(ec.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving base commit: %w", err)
	}

	session := gitutil.SessionID()
	n := len(node.Branches)

	ec.emit(tui.PhaseEvent{Title: fmt.Sprintf("Composition — %d branches, resolver: %s", n, node.Resolver)})
	ec.emit(tui.LogEvent{Level: "info", Text: "session: " + session})

	// --- Create per-branch worktrees ---
	worktrees := make([]*gitutil.Worktree, 0, n)
	cleanupWorktrees := func() {
		for _, wt := range worktrees {
			_ = gitutil.RemoveWorktree(ec.ProjectRoot, wt.Path, wt.Branch)
		}
	}
	for i := 1; i <= n; i++ {
		wtPath := filepath.Join(ec.ProjectRoot, ".braid", "race", session, fmt.Sprintf("run-%d", i))
		branch := fmt.Sprintf("braid-%s-%d", session, i)
		wt, err := gitutil.CreateWorktree(ec.ProjectRoot, wtPath, branch)
		if err != nil {
			cleanupWorktrees()
			return nil, fmt.Errorf("creating worktree run-%d: %w", i, err)
		}
		worktrees = append(worktrees, wt)
		ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("worktree run-%d: %s", i, wt.Branch)})
	}

	// --- Launch per-branch goroutines ---
	results := make([]*runResult, n)
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runCompositionBranch(ctx, idx+1, node.Branches[idx], ec, worktrees[idx])
		}(i)
	}

	wg.Wait()

	// If the parent context was cancelled (SIGINT), short-circuit to
	// cleanup: don't commit partial work, don't run the resolver.
	if ctx.Err() != nil {
		cleanupWorktrees()
		return &ExecutionResult{LastMessage: ec.LastMessage}, ctx.Err()
	}

	// --- Commit each successful branch so resolvers can diff/merge ---
	for _, r := range results {
		if r == nil || r.Status != "done" {
			continue
		}
		if err := gitutil.AddAll(r.WorktreePath); err != nil {
			ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("run %d: git add failed: %v", r.Index, err)})
			continue
		}
		if err := gitutil.Commit(r.WorktreePath, fmt.Sprintf("braid run %d", r.Index)); err != nil {
			ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("run %d: commit failed: %v", r.Index, err)})
		}
	}

	// --- Summarize ---
	ec.emit(tui.PhaseEvent{Title: "Results"})
	successful := []*runResult{}
	for _, r := range results {
		if r.Status == "done" {
			ec.emit(tui.LogEvent{Level: "info", Text: fmt.Sprintf("run %d: done (%s)", r.Index, r.BranchName)})
			successful = append(successful, r)
		} else {
			msg := r.Status
			if r.Err != nil {
				msg = r.Err.Error()
			}
			ec.emit(tui.LogEvent{Level: "error", Text: fmt.Sprintf("run %d: %s — %s", r.Index, r.Status, msg)})
		}
	}

	if len(successful) == 0 {
		for _, wt := range worktrees {
			_ = gitutil.RemoveWorktree(ec.ProjectRoot, wt.Path, wt.Branch)
		}
		return &ExecutionResult{LastMessage: ""}, errors.New("all composition branches failed")
	}

	// --- Dispatch resolver ---
	switch node.Resolver {
	case ast.ResolverPick:
		return resolvePick(ctx, node, ec, results, successful, session)
	case ast.ResolverMerge:
		return resolveMerge(ctx, node, ec, results, successful, session, baseCommit)
	case ast.ResolverCompare:
		return resolveCompare(ctx, node, ec, results, successful, session, baseCommit)
	default:
		return nil, fmt.Errorf("unknown resolver: %v", node.Resolver)
	}
}

// runCompositionBranch executes one branch inside its worktree. Each branch
// gets its own event channel that tags events with RunIndex before
// forwarding them to the parent TUI, and its own runner pool rooted at the
// worktree path.
func runCompositionBranch(ctx context.Context, runIdx int, branchNode ast.Node, parent *ExecutionContext, wt *gitutil.Worktree) *runResult {
	res := &runResult{
		Index:        runIdx,
		WorktreePath: wt.Path,
		BranchName:   wt.Branch,
		Status:       "error",
	}

	// --- Per-branch event forwarder (tags RunIndex, forwards to parent) ---
	branchEvents := make(chan tui.Event, 64)
	forwarderDone := make(chan struct{})
	go func() {
		defer close(forwarderDone)
		for ev := range branchEvents {
			tagged := tagRunIndex(ev, runIdx)
			tui.Emitter(parent.Events).Send(tagged)
		}
	}()
	defer func() {
		close(branchEvents)
		<-forwarderDone
	}()

	// --- Per-branch BRAID.md from the worktree (users can customize per branch) ---
	branchBraidMD, err := template.Load(wt.Path)
	if err != nil {
		res.Err = fmt.Errorf("loading BRAID.md: %w", err)
		return res
	}

	// --- Per-branch session log inside the worktree ---
	branchSession, err := braidlog.NewSession(wt.Path)
	if err != nil {
		res.Err = fmt.Errorf("creating session log: %w", err)
		return res
	}
	defer branchSession.Close()

	// --- Per-branch runner pool rooted at the worktree ---
	dockerCfg, _ := config.LoadDocker(parent.ProjectRoot)
	pool := runner.NewPool(BranchRunnerFactory(wt.Path, parent.Config.Env, dockerCfg))
	defer pool.StopAll()

	// --- Branch execution context ---
	branchEC := &ExecutionContext{
		ProjectRoot: wt.Path,
		Config:      parent.Config,
		Flags:       parent.Flags,
		StepConfig:  parent.StepConfig,
		BraidMD:     branchBraidMD,
		ShowRequest: parent.ShowRequest,
		LastMessage: parent.LastMessage,
		Events:      branchEvents,
	}

	result, err := execute(ctx, branchNode, branchEC, pool, branchSession)
	if err != nil {
		if ctx.Err() != nil {
			res.Status = "cancelled"
		}
		res.Err = err
		return res
	}

	res.Status = "done"
	res.LastMessage = result.LastMessage
	res.LogFile = result.LogFile
	return res
}

// tagRunIndex stamps RunIndex on the event types that carry one.
// Other event types pass through unchanged.
func tagRunIndex(ev tui.Event, runIdx int) tui.Event {
	switch e := ev.(type) {
	case tui.LineEvent:
		e.RunIndex = runIdx
		return e
	case tui.StepEvent:
		e.RunIndex = runIdx
		return e
	case tui.GateEvent:
		e.RunIndex = runIdx
		return e
	}
	return ev
}
