package executor

import (
	"context"
	"fmt"

	"github.com/ihavespoons/braid/internal/ast"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
	"github.com/ihavespoons/braid/internal/tui"
)

// executeRepeat runs node.Inner sequentially N times, threading each
// iteration's output as the next iteration's input via LastMessage.
func executeRepeat(ctx context.Context, node *ast.RepeatNode, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	result := &ExecutionResult{LastMessage: ec.LastMessage}

	for pass := 1; pass <= node.Count; pass++ {
		ec.emit(tui.PhaseEvent{Title: fmt.Sprintf("Repeat pass %d/%d", pass, node.Count)})

		inner, err := execute(ctx, node.Inner, &ExecutionContext{
			ProjectRoot:     ec.ProjectRoot,
			Config:          ec.Config,
			Flags:           ec.Flags,
			StepConfig:      ec.StepConfig,
			BraidMD:         ec.BraidMD,
			ShowRequest:     ec.ShowRequest,
			LastMessage:     result.LastMessage,
			RepeatPass:      pass,
			MaxRepeatPasses: node.Count,
			RalphIteration:  ec.RalphIteration,
			MaxRalph:        ec.MaxRalph,
			Events:          ec.Events,
		}, pool, session)
		if err != nil {
			return nil, err
		}
		result = inner
	}

	return result, nil
}
