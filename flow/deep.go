package flow

import (
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/internal/deep"
	"github.com/go-kratos/blades/tools"
)

type DeepConfig struct {
	Name          string
	Model         blades.ModelProvider
	Description   string
	Instruction   string
	Tools         []tools.Tool
	SubAgents     []blades.Agent
	MaxIterations int
	// Whether to include general-purpose agent
	WithoutGeneralSubAgent bool
}

func NewDeepAgent(config DeepConfig) (blades.Agent, error) {
	var (
		instructions = []string{deep.BaseAgentPrompt}
		subAgents    = append([]blades.Agent{}, config.SubAgents...)
		agentTools   = append([]tools.Tool{}, config.Tools...)
	)
	if len(config.Instruction) > 0 {
		instructions = append([]string{config.Instruction}, instructions...)
	}
	todosTool, todosInstruction, err := deep.NewWriteTodosTool()
	if err != nil {
		return nil, err
	}
	agentTools = append(agentTools, todosTool)
	instructions = append(instructions, todosInstruction)

	if !config.WithoutGeneralSubAgent {
		generalAgent, err := newGeneralPurposeAgent(config, instructions, agentTools)
		if err != nil {
			return nil, err
		}
		subAgents = append(subAgents, generalAgent)
	}
	tasksTool, tasksInstruction, err := deep.NewTaskTool(subAgents...)
	if err != nil {
		return nil, err
	}
	agentTools = append(agentTools, tasksTool)
	instructions = append(instructions, tasksInstruction)
	return blades.NewAgent(config.Name,
		blades.WithModel(config.Model),
		blades.WithDescription(config.Description),
		blades.WithInstruction(strings.Join(instructions, "\n")),
		blades.WithTools(agentTools...),
		blades.WithMaxIterations(config.MaxIterations),
	)
}

func newGeneralPurposeAgent(config DeepConfig, instructions []string, agentTools []tools.Tool) (blades.Agent, error) {
	return blades.NewAgent(deep.GeneralAgentName,
		blades.WithModel(config.Model),
		blades.WithDescription(deep.GeneralAgentDescription),
		blades.WithInstruction(strings.Join(instructions, "\n")),
		blades.WithTools(agentTools...),
		blades.WithMaxIterations(config.MaxIterations),
	)
}
