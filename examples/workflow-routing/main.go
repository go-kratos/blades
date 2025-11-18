package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

// RoutingWorkflow is a workflow that routes requests to different agents based on the content of the prompt.
type RoutingWorkflow struct {
	blades.Agent
	routes map[string]string
	agents map[string]blades.Agent
}

// NewRoutingWorkflow creates a new RoutingWorkflow with the given model provider and routes.
func NewRoutingWorkflow(routes map[string]string) (*RoutingWorkflow, error) {
	model := openai.NewModel("gpt-5", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	router, err := blades.NewAgent(
		"triage_agent",
		blades.WithModel(model),
		blades.WithInstructions("You determine which agent to use based on the user's homework question"),
	)
	if err != nil {
		return nil, err
	}
	agents := make(map[string]blades.Agent, len(routes))
	for name, instructions := range routes {
		agent, err := blades.NewAgent(
			name,
			blades.WithModel(model),
			blades.WithInstructions(instructions),
		)
		if err != nil {
			return nil, err
		}
		agents[name] = agent
	}
	return &RoutingWorkflow{
		Agent:  router,
		routes: routes,
		agents: agents,
	}, nil
}

// Run selects a route using the prompt content and streams from the chosen runner.
func (r *RoutingWorkflow) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		agent, err := r.selectRoute(ctx, invocation)
		if err != nil {
			yield(nil, err)
			return
		}
		stream := agent.Run(ctx, invocation)
		for msg, err := range stream {
			if !yield(msg, err) {
				break
			}
		}
	}
}

// selectRoute determines the best route key and runner for the given prompt.
func (r *RoutingWorkflow) selectRoute(ctx context.Context, invocation *blades.Invocation) (blades.Agent, error) {
	var buf strings.Builder
	buf.WriteString("You are a routing agent.\n")
	buf.WriteString("Choose the single best route key for handling the user's request.\n")
	buf.WriteString("User message: " + invocation.Message.Text() + "\n")
	buf.WriteString("Available route keys (choose exactly one):\n")
	routes, err := json.Marshal(r.routes)
	if err != nil {
		return nil, err
	}
	buf.WriteString(string(routes))
	buf.WriteString("\nOnly return the name of the routing key.")
	for res, err := range r.Agent.Run(ctx, &blades.Invocation{Message: blades.UserMessage(buf.String())}) {
		if err != nil {
			return nil, err
		}
		choice := strings.TrimSpace(res.Text())
		if a, ok := r.agents[choice]; ok {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no route selected")
}

func main() {
	var (
		routes = map[string]string{
			"math_agent": "You provide help with math problems. Explain your reasoning at each step and include examples.",
			"geo_agent":  "You provide assistance with geographical queries. Explain geographic concepts, locations, and spatial relationships clearly.",
		}
	)
	routing, err := NewRoutingWorkflow(routes)
	if err != nil {
		log.Fatal(err)
	}
	// Example prompt that will be routed to the history_agent
	input := blades.UserMessage("What is the capital of France?")
	runner := blades.NewRunner(routing)
	res, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(res.Text())
}
