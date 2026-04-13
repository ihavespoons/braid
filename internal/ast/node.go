// Package ast defines the abstract syntax tree for braid's execution pipeline
// and a parser that converts CLI positional arguments into that tree.
package ast

// Resolver identifies how a composition (race/vs) selects its winning branch.
type Resolver int

const (
	ResolverPick Resolver = iota
	ResolverMerge
	ResolverCompare
)

func (r Resolver) String() string {
	switch r {
	case ResolverPick:
		return "pick"
	case ResolverMerge:
		return "merge"
	case ResolverCompare:
		return "compare"
	default:
		return "unknown"
	}
}

// Node is a sealed interface implemented by every AST node type.
type Node interface {
	nodeMarker()
}

// WorkNode represents a single agent invocation with a prompt.
type WorkNode struct {
	Prompt string
}

// RepeatNode runs its inner node Count times sequentially, threading the
// previous iteration's output as the next iteration's input.
type RepeatNode struct {
	Inner Node
	Count int
}

// ReviewNode executes a work→review→gate loop. The inner node provides the
// initial work; subsequent iterations use IteratePrompt (or inner) while
// review uses ReviewPrompt and gate uses GatePrompt.
type ReviewNode struct {
	Inner         Node
	ReviewPrompt  string // empty = use template default
	GatePrompt    string
	IteratePrompt string
	MaxIterations int
}

// RalphNode iterates its inner node over tasks in PLAN.md until the gate
// emits DONE or MaxTasks is reached.
type RalphNode struct {
	Inner      Node
	MaxTasks   int
	GatePrompt string
}

// CompositionNode runs multiple branches in parallel and resolves them via
// Resolver (pick/merge/compare) with optional Criteria.
type CompositionNode struct {
	Branches []Node
	Resolver Resolver
	Criteria string
}

func (*WorkNode) nodeMarker()        {}
func (*RepeatNode) nodeMarker()      {}
func (*ReviewNode) nodeMarker()      {}
func (*RalphNode) nodeMarker()       {}
func (*CompositionNode) nodeMarker() {}
