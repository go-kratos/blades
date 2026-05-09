package flow

import (
	"context"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// RoutingConfig configures a routing agent.
type RoutingConfig struct {
	Name        string
	Description string
	SubAgents   []blades.Agent
	Router      Router
}

// Router selects which sub-agent to delegate to.
type Router interface {
	Route(ctx context.Context, input event.Input, agents []blades.Agent) (blades.Agent, error)
}

// RouterFunc is a function adapter for Router.
type RouterFunc func(ctx context.Context, input event.Input, agents []blades.Agent) (blades.Agent, error)

func (f RouterFunc) Route(ctx context.Context, input event.Input, agents []blades.Agent) (blades.Agent, error) {
	return f(ctx, input, agents)
}

// NewRoutingAgent creates an agent that routes to a sub-agent based on input.
func NewRoutingAgent(cfg RoutingConfig) (blades.Agent, error) {
	if cfg.Router == nil {
		return nil, ErrRouterRequired
	}
	return &routingAgent{cfg: cfg}, nil
}

type routingAgent struct {
	cfg RoutingConfig
}

func (a *routingAgent) Name() string        { return a.cfg.Name }
func (a *routingAgent) Description() string { return a.cfg.Description }

func (a *routingAgent) Run(ctx context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	output := make(chan event.Output, 64)
	go a.run(ctx, input, output)
	return output, nil
}

func (a *routingAgent) run(ctx context.Context, input <-chan event.Input, output chan<- event.Output) {
	defer func() {
		output <- event.Done{}
		close(output)
	}()

	var firstInput event.Input
	for in := range input {
		firstInput = in
		break
	}
	if firstInput == nil {
		return
	}

	target, err := a.cfg.Router.Route(ctx, firstInput, a.cfg.SubAgents)
	if err != nil {
		output <- event.Error{Err: err}
		return
	}

	ch := make(chan event.Input, 1)
	ch <- firstInput
	close(ch)

	subOut, err := target.Run(ctx, ch)
	if err != nil {
		output <- event.Error{Err: err}
		return
	}
	for o := range subOut {
		if _, ok := o.(event.Done); ok {
			continue
		}
		output <- o
	}
}
