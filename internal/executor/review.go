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

// Default prompts are used when the AST did not specify one. They give
// braid sensible behavior out of the box without requiring a custom
// BRAID.md template.
const (
	defaultReviewPrompt = `Review the work done in the previous step.
Check the session log for what changed.
Identify issues categorized as High, Medium, or Low severity.`

	defaultGatePrompt = `Based on the review, respond with exactly DONE or ITERATE
on its own line, followed by a brief reason.

DONE if: the work is complete and no High severity issues remain.
ITERATE if: there are High severity issues or the work is incomplete.`

	defaultIteratePrompt = `Continue working on the task based on the review feedback.`
)

// loopStep is a single entry in the review loop sequence.
type loopStep struct {
	Name   config.StepName
	Prompt string
}

// executeReview runs the work→review→gate loop. If node.Inner is a WorkNode
// we use its prompt directly; otherwise the inner node runs first and its
// output becomes the LastMessage for an iterate-review-gate loop (with the
// first work step skipped, since inner already produced initial output).
func executeReview(ctx context.Context, node *ast.ReviewNode, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	reviewPrompt := node.ReviewPrompt
	if reviewPrompt == "" {
		reviewPrompt = defaultReviewPrompt
	}
	gatePrompt := node.GatePrompt
	if gatePrompt == "" {
		gatePrompt = defaultGatePrompt
	}
	iteratePrompt := node.IteratePrompt
	if iteratePrompt == "" {
		iteratePrompt = defaultIteratePrompt
	}

	var (
		initialLastMessage = ec.LastMessage
		skipFirstWork      = false
		workPrompt         string
	)

	if inner, ok := node.Inner.(*ast.WorkNode); ok {
		workPrompt = inner.Prompt
	} else {
		// Compound inner node: execute it first, then review-loop its output.
		innerResult, err := execute(ctx, node.Inner, ec, pool, session)
		if err != nil {
			return nil, err
		}
		initialLastMessage = innerResult.LastMessage
		skipFirstWork = true
		workPrompt = iteratePrompt
	}

	return runAgentLoop(ctx, agentLoopConfig{
		WorkPrompt:         workPrompt,
		ReviewPrompt:       reviewPrompt,
		GatePrompt:         gatePrompt,
		IteratePrompt:      iteratePrompt,
		MaxIterations:      node.MaxIterations,
		InitialLastMessage: initialLastMessage,
		SkipFirstWork:      skipFirstWork,
	}, ec, pool, session)
}

type agentLoopConfig struct {
	WorkPrompt         string
	ReviewPrompt       string
	GatePrompt         string
	IteratePrompt      string
	MaxIterations      int
	InitialLastMessage string
	SkipFirstWork      bool
}

// runAgentLoop executes the work→review→gate loop up to MaxIterations,
// returning a DONE verdict on a passing gate or MAX_ITERATIONS on
// exhaustion.
func runAgentLoop(ctx context.Context, cfg agentLoopConfig, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	lastMessage := cfg.InitialLastMessage

	for iter := 1; iter <= cfg.MaxIterations; iter++ {
		ec.emit(tui.PhaseEvent{Title: fmt.Sprintf("Iteration %d/%d", iter, cfg.MaxIterations)})

		// Build the ordered step list for this iteration.
		var steps []loopStep
		if !(iter == 1 && cfg.SkipFirstWork) {
			stepName := config.StepWork
			prompt := cfg.WorkPrompt
			if iter > 1 && cfg.IteratePrompt != "" {
				stepName = config.StepIterate
				prompt = cfg.IteratePrompt
			}
			steps = append(steps, loopStep{Name: stepName, Prompt: prompt})
		}
		steps = append(steps,
			loopStep{Name: config.StepReview, Prompt: cfg.ReviewPrompt},
			loopStep{Name: config.StepGate, Prompt: cfg.GatePrompt},
		)

		for _, step := range steps {
			stepSel := ec.StepConfig[step.Name]
			ec.emit(tui.StepEvent{
				Step:          string(step.Name),
				Agent:         string(stepSel.Agent),
				Model:         stepSel.Model,
				Iteration:     iter,
				MaxIterations: cfg.MaxIterations,
			})

			prompt, err := template.Render(ec.BraidMD, template.LoopContext{
				Step:            string(step.Name),
				Prompt:          step.Prompt,
				LastMessage:     lastMessage,
				Iteration:       iter,
				MaxIterations:   cfg.MaxIterations,
				LogFile:         session.Path,
				RalphIteration:  ec.RalphIteration,
				MaxRalph:        ec.MaxRalph,
				RepeatPass:      ec.RepeatPass,
				MaxRepeatPasses: ec.MaxRepeatPasses,
			})
			if err != nil {
				return nil, fmt.Errorf("rendering %s template: %w", step.Name, err)
			}

			if ec.ShowRequest {
				ec.emit(tui.PromptEvent{Text: prompt})
			}

			runnerForMode, err := pool.Get(stepSel.Sandbox)
			if err != nil {
				return nil, fmt.Errorf("getting runner for %s: %w", step.Name, err)
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
				return runnerForMode.RunAgent(ctx, stepSel.Agent, stepSel.Model, prompt, onLine)
			})
			if err != nil {
				return &ExecutionResult{
					LastMessage: lastMessage,
					LogFile:     session.Path,
					Verdict:     VerdictIterate,
					Iterations:  iter,
				}, fmt.Errorf("%s step failed (iteration %d): %w", step.Name, iter, err)
			}

			lastMessage = output
			if logErr := session.Append(string(step.Name), iter, output); logErr != nil {
				ec.emit(tui.LogEvent{Level: "warn", Text: fmt.Sprintf("failed to append session log: %v", logErr)})
			}
		}

		verdict := ParseGateVerdict(lastMessage)
		if verdict == VerdictDone {
			ec.emit(tui.GateEvent{Verdict: "DONE", Message: "loop complete"})
			return &ExecutionResult{
				LastMessage: lastMessage,
				LogFile:     session.Path,
				Verdict:     VerdictDone,
				Iterations:  iter,
			}, nil
		}
		if iter < cfg.MaxIterations {
			ec.emit(tui.GateEvent{Verdict: "ITERATE", Message: fmt.Sprintf("continuing to iteration %d", iter+1)})
		}
	}

	ec.emit(tui.GateEvent{Verdict: "MAX_ITERATIONS", Message: fmt.Sprintf("%d iterations reached", cfg.MaxIterations)})
	return &ExecutionResult{
		LastMessage: lastMessage,
		LogFile:     session.Path,
		Verdict:     VerdictMaxIterations,
		Iterations:  cfg.MaxIterations,
	}, nil
}
