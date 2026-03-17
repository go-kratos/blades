// Package recipe provides a declarative YAML-based system for defining
// and building blades.Agent workflows. An agent spec is a YAML specification
// that describes an agent (or a pipeline of agents) including model selection,
// instructions, parameters, context management, middlewares, and sub-agents
// for multi-step workflows.
//
// Usage:
//
//	// Register models
//	registry := recipe.NewRegistry()
//	registry.Register("gpt-4o", myModelProvider)
//
//	// Load and build
//	spec, err := recipe.LoadFromFile("agent.yaml")
//	agent, err := recipe.Build(spec,
//	    recipe.WithModelRegistry(registry),
//	    recipe.WithParams(map[string]any{"language": "go"}),
//	)
//
//	// Run normally
//	runner := blades.NewRunner(agent)
//	output, err := runner.Run(ctx, blades.UserMessage("Review this code"))
package recipe
