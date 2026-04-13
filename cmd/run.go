package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	"github.com/ihavespoons/braid/internal/executor"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/sandbox"
	"github.com/ihavespoons/braid/internal/template"
	"github.com/ihavespoons/braid/internal/tui"
)

// RunDefaultArgs is the entry point for the default run command. It is
// invoked directly from main when argv[1] is not a recognized subcommand,
// bypassing cobra so that reserved keywords (review, vs, pick, ...) can
// appear as positional arguments without triggering cobra's subcommand
// resolution.
func RunDefaultArgs(args []string) {
	if err := runDefault(args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runDefault(args []string) error {
	node, flags, err := ast.Parse(args)
	if err != nil {
		return err
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, warn := config.Load(projectRoot)
	if warn != nil {
		braidlog.Warn("%v", warn)
	}

	braidMD, err := template.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("loading BRAID.md: %w\nhint: braid uses Go text/template syntax ({{.Var}})", err)
	}

	// --- Signal handling ---
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Runner pool ---
	dockerCfg, dockerWarn := config.LoadDocker(projectRoot)
	if dockerWarn != nil {
		braidlog.Warn("%v", dockerWarn)
	}
	envPassthrough := cfg.Env
	pool := runner.NewPool(MakeRunnerFactory(projectRoot, envPassthrough, dockerCfg))

	go func() {
		<-ctx.Done()
		_ = pool.StopAll()
	}()

	// --- Session log ---
	session, err := braidlog.NewSession(projectRoot)
	if err != nil {
		return err
	}

	// --- TUI vs plain-log selection ---
	useTUI := isatty.IsTerminal(os.Stdout.Fd()) && os.Getenv("BRAID_NO_TUI") == ""

	events := make(chan tui.Event, 64)
	stepConfig := executor.ResolveStepConfig(&cfg, &flags)
	ec := &executor.ExecutionContext{
		ProjectRoot: projectRoot,
		Config:      &cfg,
		Flags:       &flags,
		StepConfig:  stepConfig,
		BraidMD:     braidMD,
		ShowRequest: flags.ShowRequest,
		Events:      events,
	}

	if useTUI {
		return runWithTUI(ctx, node, ec, pool, session, events)
	}
	return runWithLogging(ctx, node, ec, pool, session, events)
}

// runWithTUI executes the pipeline with a bubbletea front-end. When the
// root AST node is a composition it uses the multi-panel RaceModel;
// otherwise it uses the single-run AppModel.
func runWithTUI(
	ctx context.Context,
	node ast.Node,
	ec *executor.ExecutionContext,
	pool *runner.Pool,
	session *braidlog.Session,
	events chan tui.Event,
) error {
	var model tea.Model
	if comp, ok := node.(*ast.CompositionNode); ok {
		model = tui.NewRaceModel("Braid composition — "+sessionHeader(session.Path), len(comp.Branches))
	} else {
		model = tui.NewAppModel("Braid — " + sessionHeader(session.Path)).WithShowRequest(ec.ShowRequest)
	}
	p := tea.NewProgram(model, tea.WithContext(ctx), tea.WithOutput(os.Stderr))

	// Pump events from the executor into the tea Program. Runs for the
	// lifetime of the channel.
	go func() {
		for ev := range events {
			p.Send(ev)
		}
	}()

	// Run the executor in a goroutine so we can start the bubbletea loop
	// on the main goroutine (required for proper TTY handling).
	var (
		execResult *executor.ExecutionResult
		execErr    error
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		execResult, execErr = executor.Execute(ctx, node, ec, pool, session)
		// Emit DoneEvent so the TUI can transition to its final view
		// and call tea.Quit. We still close events afterward to stop
		// the pumper goroutine.
		verdict := ""
		iterations := 0
		logFile := ""
		lastMessage := ""
		if execResult != nil {
			verdict = string(execResult.Verdict)
			iterations = execResult.Iterations
			logFile = execResult.LogFile
			lastMessage = execResult.LastMessage
		}
		if execErr != nil {
			events <- tui.ErrorEvent{Err: execErr.Error()}
		}
		events <- tui.DoneEvent{
			LastMessage: lastMessage,
			LogFile:     logFile,
			Verdict:     verdict,
			Iterations:  iterations,
		}
		close(events)
	}()

	if _, err := p.Run(); err != nil {
		<-done
		return err
	}
	<-done
	_ = pool.StopAll()
	return execErr
}

// runWithLogging is the fallback for non-TTY environments (pipes, CI).
// It translates events into the existing braidlog format so output stays
// useful when piped to a file.
func runWithLogging(
	ctx context.Context,
	node ast.Node,
	ec *executor.ExecutionContext,
	pool *runner.Pool,
	session *braidlog.Session,
	events chan tui.Event,
) error {
	braidlog.Phase("Braid — " + sessionHeader(session.Path))

	// Consume events in a goroutine, mirroring them to braidlog.
	go func() {
		for ev := range events {
			logEvent(ev)
		}
	}()

	result, err := executor.Execute(ctx, node, ec, pool, session)
	close(events)
	_ = pool.StopAll()

	if err != nil {
		return err
	}
	printFinalStatus(result)
	return nil
}

// logEvent formats a single tui.Event via the braidlog package.
func logEvent(ev tui.Event) {
	switch e := ev.(type) {
	case tui.PhaseEvent:
		braidlog.Phase(e.Title)
	case tui.StepEvent:
		braidlog.Step("%s — agent=%s model=%s", e.Step, e.Agent, e.Model)
	case tui.LineEvent:
		braidlog.Info("  %s", e.Text)
	case tui.GateEvent:
		switch e.Verdict {
		case "DONE":
			braidlog.OK("Gate: DONE — %s", e.Message)
		case "MAX_ITERATIONS":
			braidlog.Warn("Gate: MAX_ITERATIONS — %s", e.Message)
		default:
			braidlog.Warn("Gate: %s — %s", e.Verdict, e.Message)
		}
	case tui.WaitingEvent:
		braidlog.Warn("rate limited; retry at %s (attempt %d)", e.NextRetryAt.Format("15:04:05"), e.Attempt)
	case tui.RetryEvent:
		braidlog.Step("retrying (attempt %d)...", e.Attempt)
	case tui.LogEvent:
		switch e.Level {
		case "warn":
			braidlog.Warn("%s", e.Text)
		case "error":
			braidlog.Error("%s", e.Text)
		default:
			braidlog.Info("%s", e.Text)
		}
	case tui.ErrorEvent:
		braidlog.Error("%s", e.Err)
	}
}

func printFinalStatus(result *executor.ExecutionResult) {
	if result == nil {
		return
	}
	switch result.Verdict {
	case executor.VerdictDone:
		braidlog.OK("Completed in %d iteration(s) — log: %s", result.Iterations, result.LogFile)
	case executor.VerdictMaxIterations:
		braidlog.Warn("Max iterations reached (%d) — log: %s", result.Iterations, result.LogFile)
	case executor.VerdictIterate:
		braidlog.Warn("Ended mid-loop — log: %s", result.LogFile)
	case executor.VerdictNone:
		if result.LogFile != "" {
			braidlog.OK("Done — log: %s", result.LogFile)
		}
	}
}

// sessionHeader returns the basename of the session log path for use as a
// compact TUI banner title.
func sessionHeader(path string) string {
	return filepath.Base(path)
}

// MakeRunnerFactory returns a runner.Factory that dispatches based on the
// sandbox mode: "agent" (native subprocess) or "docker" (sandbox image).
// Exported so the composition/resolver code can reuse it for per-worktree
// pools.
func MakeRunnerFactory(projectRoot string, envPassthrough []string, dockerCfg config.DockerConfig) runner.Factory {
	return func(mode config.SandboxMode) (runner.AgentRunner, error) {
		switch mode {
		case config.SandboxDocker:
			return sandbox.NewDockerRunner(projectRoot, envPassthrough, dockerCfg), nil
		default:
			return runner.NewNative(projectRoot, envPassthrough), nil
		}
	}
}
