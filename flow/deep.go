package flow

import (
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/deep"
	"github.com/go-kratos/blades/tools"
)

type DeepConfig struct {
	Name                   string
	Model                  blades.ModelProvider
	Description            string
	Instruction            string
	Tools                  []tools.Tool
	SubAgents              []blades.Agent
	MaxIterations          int
	WithoutGeneralSubAgent bool
}

// NewDeepAgent constructs and returns a "deep agent" using the provided configuration.
// A deep agent is an advanced agent capable of managing complex tasks, maintaining a list of todos,
// and delegating work to subagents. Unlike a regular agent, a deep agent supports hierarchical
// delegation, allowing it to break down tasks and assign them to specialized subagents as needed.
// The returned agent can manage its own todos, utilize custom tools, and coordinate with subagents
// to accomplish multi-step or collaborative objectives.
func NewDeepAgent(config DeepConfig) (blades.Agent, error) {
	tc := deep.TaskToolConfig{
		Model:                  config.Model,
		Instructions:           []string{config.Instruction, deep.BaseAgentPrompt},
		Tools:                  config.Tools,
		SubAgents:              config.SubAgents,
		MaxIterations:          config.MaxIterations,
		WithoutGeneralSubAgent: config.WithoutGeneralSubAgent,
	}
	todosTool, todosInstruction, err := deep.NewWriteTodosTool()
	if err != nil {
		return nil, err
	}
	tc.Tools = append(tc.Tools, todosTool)
	tc.Instructions = append(tc.Instructions, todosInstruction)
	if !tc.WithoutGeneralSubAgent || len(tc.SubAgents) > 0 {
		taskTool, taskInstruction, err := deep.NewTaskTool(tc)
		if err != nil {
			return nil, err
		}
		tc.Tools = append(tc.Tools, taskTool)
		tc.Instructions = append(tc.Instructions, taskInstruction)
	}
	return blades.NewAgent(config.Name,
		blades.WithModel(config.Model),
		blades.WithDescription(config.Description),
		blades.WithInstruction(strings.Join(tc.Instructions, "\n\n")),
		blades.WithTools(tc.Tools...),
		blades.WithMaxIterations(config.MaxIterations),
	)
}
