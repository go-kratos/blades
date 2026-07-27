package hook

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
)

// Hook defines lifecycle callbacks for the agent loop. A turn wraps one
// primary model call and the tool wave produced by its response.
type Hook interface {
	BeforeModel(ctx context.Context, req *model.Request) error
	AfterModel(ctx context.Context, req *model.Request, resp *model.Response, err error) error
	BeforeTool(ctx context.Context, call *ToolCall) error
	AfterTool(ctx context.Context, call *ToolCall, result *tools.Result, err error) error
	BeforeTurn(ctx context.Context, t *Turn) error
	AfterTurn(ctx context.Context, t *Turn, summary *TurnSummary, err error) error
}

// Noop is an embeddable default implementation of Hook.
type Noop struct{}

func (Noop) BeforeModel(_ context.Context, _ *model.Request) error { return nil }
func (Noop) AfterModel(_ context.Context, _ *model.Request, _ *model.Response, _ error) error {
	return nil
}
func (Noop) BeforeTool(_ context.Context, _ *ToolCall) error                          { return nil }
func (Noop) AfterTool(_ context.Context, _ *ToolCall, _ *tools.Result, _ error) error { return nil }
func (Noop) BeforeTurn(_ context.Context, _ *Turn) error                              { return nil }
func (Noop) AfterTurn(_ context.Context, _ *Turn, _ *TurnSummary, _ error) error      { return nil }

// ToolCall carries context for BeforeTool/AfterTool hooks.
type ToolCall struct {
	ID        string
	AgentName string
	Turn      int
	Tool      tools.Tool
	Input     json.RawMessage
}

// Turn carries context for BeforeTurn/AfterTurn hooks. Input is nil for a turn
// started only by tool results from the preceding turn.
type Turn struct {
	AgentName string
	Turn      int
	Input     event.Input
}

// TurnSummary contains call-local results for a completed turn.
type TurnSummary struct {
	Parts      []content.Part
	StopReason model.StopReason
	Usage      *model.Usage
}
