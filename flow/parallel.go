package flow

import (
	"context"
	"sync"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// ParallelConfig configures a parallel agent.
type ParallelConfig struct {
	Name        string
	Description string
	SubAgents   []blades.Agent
}

// NewParallelAgent creates an agent that runs sub-agents concurrently.
func NewParallelAgent(cfg ParallelConfig) blades.Agent {
	return &parallelAgent{cfg: cfg}
}

type parallelAgent struct {
	cfg ParallelConfig
}

func (a *parallelAgent) Name() string        { return a.cfg.Name }
func (a *parallelAgent) Description() string { return a.cfg.Description }

func (a *parallelAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	output := make(chan event.Output, 64)
	go a.run(ctx, input, output)
	return output, nil
}

func (a *parallelAgent) run(ctx context.Context, input <-chan event.Input, output chan<- event.Output) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	// Collect initial input
	var firstInput event.Input
	for in := range input {
		firstInput = in
		break
	}
	if firstInput == nil {
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, sub := range a.cfg.SubAgents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan event.Input, 1)
			ch <- firstInput
			close(ch)

			subOut, err := sub.Run(ctx, ch)
			if err != nil {
				mu.Lock()
				output <- event.Error{Err: err}
				mu.Unlock()
				return
			}
			for o := range subOut {
				if _, ok := o.(event.Done); ok {
					continue
				}
				mu.Lock()
				output <- o
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}
