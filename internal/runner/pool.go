package runner

import (
	"errors"
	"sync"

	"github.com/ihavespoons/braid/internal/config"
)

// Pool holds lazily-created AgentRunners keyed by sandbox mode. The pool
// ensures that multiple steps in the same pipeline share a single runner
// per mode, avoiding repeated setup costs (container creation, etc.).
type Pool struct {
	factory Factory

	mu      sync.Mutex
	runners map[config.SandboxMode]AgentRunner
	stopped bool
}

// NewPool creates an empty pool that will use factory to construct runners
// on demand.
func NewPool(factory Factory) *Pool {
	return &Pool{
		factory: factory,
		runners: map[config.SandboxMode]AgentRunner{},
	}
}

// Get returns the cached runner for mode, creating one via the factory on
// first access. Returns an error if the pool has been stopped.
func (p *Pool) Get(mode config.SandboxMode) (AgentRunner, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil, errors.New("runner pool has been stopped")
	}
	if r, ok := p.runners[mode]; ok {
		return r, nil
	}
	r, err := p.factory(mode)
	if err != nil {
		return nil, err
	}
	p.runners[mode] = r
	return r, nil
}

// StopAll stops every runner in the pool, returning the first error
// encountered (if any). Subsequent Get calls will fail.
func (p *Pool) StopAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true

	var firstErr error
	for _, r := range p.runners {
		if err := r.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.runners = nil
	return firstErr
}
