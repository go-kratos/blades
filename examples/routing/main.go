package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

// RoutingWorkflow is a workflow that routes requests to different agents based on the content of the prompt.
type RoutingWorkflow struct {
	router blades.Runner
	routes map[string]string
	agents map[string]blades.Runner
}

// NewRoutingWorkflow creates a new RoutingWorkflow with the given model provider and routes.
func NewRoutingWorkflow(routes map[string]string) *RoutingWorkflow {
	provider := openai.NewChatProvider()
	router := blades.NewAgent(
		"triage_agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions("You determine which agent to use based on the user's homework question"),
	)
	agents := make(map[string]blades.Runner, len(routes))
	for name, instructions := range routes {
		agents[name] = blades.NewAgent(
			name,
			blades.WithModel("gpt-5"),
			blades.WithProvider(provider),
			blades.WithInstructions(instructions),
		)
	}
	return &RoutingWorkflow{
		router: router,
		routes: routes,
		agents: agents,
	}
}

// Run selects a route using the prompt content and executes the chosen runner.
func (r *RoutingWorkflow) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
	runner, err := r.selectRoute(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return runner.Run(ctx, prompt, opts...)
}

// RunStream selects a route using the prompt content and streams from the chosen runner.
func (r *RoutingWorkflow) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
	runner, err := r.selectRoute(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return runner.RunStream(ctx, prompt, opts...)
}

// selectRoute determines the best route key and runner for the given prompt.
func (r *RoutingWorkflow) selectRoute(ctx context.Context, prompt *blades.Prompt) (blades.Runner, error) {
	var buf strings.Builder
	buf.WriteString("You are a routing agent.\n")
	buf.WriteString("Choose the single best route key for handling the user's request.\n")
	buf.WriteString("User message: " + prompt.String() + "\n")
	buf.WriteString("Available route keys (choose exactly one):\n")
	routes, err := json.Marshal(r.routes)
	if err != nil {
		return nil, err
	}
	buf.WriteString(string(routes))
	res, err := r.router.Run(ctx, blades.NewPrompt(
		blades.UserMessage(buf.String()),
	))
	if err != nil {
		return nil, err
	}
	choice := strings.TrimSpace(res.Text())
	return r.agents[choice], nil
}

func main() {
	var (
		routes = map[string]string{
			"math_agent":    "You provide help with math problems. Explain your reasoning at each step and include examples.",
			"history_agent": "You provide assistance with historical queries. Explain important events and context clearly.",
		}
	)
	routing := NewRoutingWorkflow(routes)
	// Example prompt that will be routed to the history_agent
	prompt := blades.NewPrompt(
		blades.UserMessage("What is the capital of France?"),
	)
	res, err := routing.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(res.Text())
}
