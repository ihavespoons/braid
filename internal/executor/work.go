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

// executeWork runs a single agent invocation using the "work" step config.
func executeWork(ctx context.Context, node *ast.WorkNode, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	stepSel := ec.StepConfig[config.StepWork]

	ec.emit(tui.StepEvent{
		Step:          string(config.StepWork),
		Agent:         string(stepSel.Agent),
		Model:         stepSel.Model,
		Iteration:     1,
		MaxIterations: 1,
	})

	prompt, err := template.Render(ec.BraidMD, template.LoopContext{
		Step:            string(config.StepWork),
		Prompt:          node.Prompt,
		LastMessage:     ec.LastMessage,
		Iteration:       1,
		MaxIterations:   1,
		LogFile:         session.Path,
		RalphIteration:  ec.RalphIteration,
		MaxRalph:        ec.MaxRalph,
		RepeatPass:      ec.RepeatPass,
		MaxRepeatPasses: ec.MaxRepeatPasses,
	})
	if err != nil {
		return nil, fmt.Errorf("rendering work template: %w", err)
	}

	if ec.ShowRequest {
		ec.emit(tui.PromptEvent{Text: prompt})
	}

	runnerForMode, err := pool.Get(stepSel.Sandbox)
	if err != nil {
		return nil, fmt.Errorf("getting runner: %w", err)
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
		return runnerForMode.RunAgent(ctx, runner.Invocation{
			Agent:          stepSel.Agent,
			Model:          stepSel.Model,
			PermissionMode: stepSel.Permissions,
			Prompt:         prompt,
			OnLine:         onLine,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("work step failed: %w", err)
	}

	if logErr := session.Append(string(config.StepWork), 1, output); logErr != nil {
		ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("failed to append session log: %v", logErr)})
	}

	return &ExecutionResult{LastMessage: output, LogFile: session.Path}, nil
}
