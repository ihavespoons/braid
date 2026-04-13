package executor

import (
	"context"
	"fmt"

	"github.com/ihavespoons/braid/internal/ast"
	braidlog "github.com/ihavespoons/braid/internal/log"
	"github.com/ihavespoons/braid/internal/runner"
)

// Execute is the top-level entry point: it dispatches the root node and
// ensures session log and pool cleanup run on exit.
func Execute(ctx context.Context, node ast.Node, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (result *ExecutionResult, err error) {
	defer func() {
		if cerr := session.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return execute(ctx, node, ec, pool, session)
}

// execute is the recursive dispatch. Unexported so callers go through
// Execute for lifecycle handling.
func execute(ctx context.Context, node ast.Node, ec *ExecutionContext, pool *runner.Pool, session *braidlog.Session) (*ExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch n := node.(type) {
	case *ast.WorkNode:
		return executeWork(ctx, n, ec, pool, session)
	case *ast.RepeatNode:
		return executeRepeat(ctx, n, ec, pool, session)
	case *ast.ReviewNode:
		return executeReview(ctx, n, ec, pool, session)
	case *ast.RalphNode:
		return executeRalph(ctx, n, ec, pool, session)
	case *ast.CompositionNode:
		return executeComposition(ctx, n, ec, pool, session)
	default:
		return nil, fmt.Errorf("unknown AST node type: %T", node)
	}
}
