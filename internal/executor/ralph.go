package executor

import (
	"context"
	"fmt"

	"github.com/ihavespoons/braid/internal/ast"
	"github.com/ihavespoons/braid/internal/config"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/retry"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/template"
	"github.com/ihavespoons/braid/internal/tui"
)

// executeRalph is the outer task-progression loop. Each iteration runs the
// inner node (typically a review loop) then asks a gate agent whether more
// tasks remain. DONE stops ralph; NEXT continues. If the inner loop exhausts
// its own MaxIterations, ralph also stops (not converging).
func executeRalph(ctx context.Context, node *ast.RalphNode, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	result := &ExecutionResult{LastMessage: ec.LastMessage}

	// ralph step falls back to gate when no ralph-specific config is set —
	// ResolveStepConfig already applied that fallback.
	ralphStep := ec.StepConfig[config.StepRalph]

	for task := 1; task <= node.MaxTasks; task++ {
		ec.emit(tui.PhaseEvent{Title: fmt.Sprintf("Ralph task %d/%d", task, node.MaxTasks)})

		// Execute the inner node with updated ralph counters threaded through.
		inner, err := execute(ctx, node.Inner, &ExecutionContext{
			ProjectRoot:     ec.ProjectRoot,
			Config:          ec.Config,
			Flags:           ec.Flags,
			StepConfig:      ec.StepConfig,
			BraidMD:         ec.BraidMD,
			ShowRequest:     ec.ShowRequest,
			LastMessage:     result.LastMessage,
			RepeatPass:      ec.RepeatPass,
			MaxRepeatPasses: ec.MaxRepeatPasses,
			RalphIteration:  task,
			MaxRalph:        node.MaxTasks,
			Events:          ec.Events,
		}, pool, session)
		if err != nil {
			return nil, err
		}
		result = inner

		// If the inner review loop gave up, so does ralph.
		if inner.Verdict == VerdictMaxIterations {
			ec.emit(tui.LogEvent{
				Level: "warn",
				Text:  fmt.Sprintf("Ralph: inner loop hit max iterations on task %d — stopping (not converging)", task),
			})
			return result, nil
		}

		// Run the ralph gate: a single agent call asking NEXT vs DONE.
		ec.emit(tui.StepEvent{
			Step:          string(config.StepRalph),
			Agent:         string(ralphStep.Agent),
			Model:         ralphStep.Model,
			Iteration:     task,
			MaxIterations: node.MaxTasks,
		})

		prompt, err := template.Render(ec.BraidMD, template.LoopContext{
			Step:            string(config.StepRalph),
			Prompt:          node.GatePrompt,
			LastMessage:     result.LastMessage,
			Iteration:       task,
			MaxIterations:   node.MaxTasks,
			LogFile:         session.Path,
			RalphIteration:  task,
			MaxRalph:        node.MaxTasks,
			RepeatPass:      ec.RepeatPass,
			MaxRepeatPasses: ec.MaxRepeatPasses,
		})
		if err != nil {
			return nil, fmt.Errorf("rendering ralph template: %w", err)
		}

		if ec.ShowRequest {
			ec.emit(tui.PromptEvent{Text: prompt})
		}

		runnerForMode, err := pool.Get(ralphStep.Sandbox)
		if err != nil {
			return nil, fmt.Errorf("getting ralph runner: %w", err)
		}

		onLine := func(line string) { ec.emit(tui.LineEvent{Text: line}) }

		output, err := retry.Do(ctx, ec.Config.Retry, retry.Options{
			OnWaiting: func(info retry.WaitingInfo) {
				ec.emit(tui.WaitingEvent{NextRetryAt: info.NextRetryAt, Attempt: info.Attempt, Err: info.Err.Error()})
			},
			OnRetry: func(info retry.RetryInfo) {
				ec.emit(tui.RetryEvent{Attempt: info.Attempt})
			},
		}, func() (string, error) {
			return runnerForMode.RunAgent(ctx, ralphStep.Agent, ralphStep.Model, prompt, onLine)
		})
		if err != nil {
			return nil, fmt.Errorf("ralph gate failed on task %d: %w", task, err)
		}

		if logErr := session.Append(string(config.StepRalph), task, output); logErr != nil {
			ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("failed to append session log: %v", logErr)})
		}

		verdict := ParseRalphVerdict(output)
		if verdict == RalphVerdictDone {
			ec.emit(tui.GateEvent{Verdict: "DONE", Message: fmt.Sprintf("ralph complete after %d task(s)", task)})
			return &ExecutionResult{
				LastMessage: output,
				LogFile:     session.Path,
				Verdict:     VerdictDone,
				Iterations:  task,
			}, nil
		}
		ec.emit(tui.GateEvent{Verdict: "NEXT", Message: fmt.Sprintf("continuing to task %d", task+1)})
	}

	ec.emit(tui.LogEvent{
		Level: "warn",
		Text:  fmt.Sprintf("Ralph: max tasks (%d) reached — stopping", node.MaxTasks),
	})
	return &ExecutionResult{
		LastMessage: result.LastMessage,
		LogFile:     session.Path,
		Verdict:     VerdictMaxIterations,
		Iterations:  node.MaxTasks,
	}, nil
}
