package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
)

type routeSelectorModel struct{}

func (m *routeSelectorModel) Name() string { return "selector" }

func (m *routeSelectorModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	msg := blades.NewAssistantMessage(blades.StatusCompleted)
	msg.Role = blades.RoleTool
	msg.Parts = append(msg.Parts, blades.ToolPart{
		ID:      "handoff-1",
		Name:    "handoff_to_agent",
		Request: `{"agentName":"worker"}`,
	})
	return &blades.ModelResponse{Message: msg}, nil
}

func (m *routeSelectorModel) NewStreaming(context.Context, *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return nil
}

type captureToolsModel struct {
	toolNames []string
}

func (m *captureToolsModel) Name() string { return "worker" }

func (m *captureToolsModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	m.toolNames = m.toolNames[:0]
	for _, tool := range req.Tools {
		m.toolNames = append(m.toolNames, tool.Name())
	}
	msg := blades.NewAssistantMessage(blades.StatusCompleted)
	msg.Parts = append(msg.Parts, blades.TextPart{Text: "done"})
	return &blades.ModelResponse{Message: msg}, nil
}

func (m *captureToolsModel) NewStreaming(context.Context, *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return nil
}

func TestRoutingAgent_DoesNotLeakRouterToolsToTarget(t *testing.T) {
	t.Parallel()

	targetModel := &captureToolsModel{}
	targetAgent, err := blades.NewAgent("worker", blades.WithModel(targetModel))
	if err != nil {
		t.Fatalf("create target agent: %v", err)
	}

	router, err := NewRoutingAgent(RoutingConfig{
		Name:      "router",
		Model:     &routeSelectorModel{},
		SubAgents: []blades.Agent{targetAgent},
	})
	if err != nil {
		t.Fatalf("create routing agent: %v", err)
	}

	runner := blades.NewRunner(router)
	if _, err := runner.Run(context.Background(), blades.UserMessage("route this")); err != nil {
		t.Fatalf("run routing agent: %v", err)
	}

	for _, name := range targetModel.toolNames {
		if name == "handoff_to_agent" {
			t.Fatalf("unexpected leaked tool %q in target invocation", name)
		}
	}
}
