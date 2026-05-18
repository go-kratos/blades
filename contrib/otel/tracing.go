package otel

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
)

const (
	traceScope    = "blades"
	defaultSystem = "other"
)

// TraceOption configures tracing hooks.
type TraceOption func(*TracingHook)

// TracingHook records agent loop lifecycle spans.
type TracingHook struct {
	hook.Noop

	system string
	tracer trace.Tracer

	mu     sync.Mutex
	turns  map[turnKey]trace.Span
	models map[*model.Request]trace.Span
	tools  map[toolKey]trace.Span
}

type turnKey struct {
	agent string
	turn  int
}

type toolKey struct {
	agent string
	turn  int
	id    string
	name  string
}

// WithSystem sets the AI system name for tracing, e.g. "openai", "claude", "gemini".
func WithSystem(system string) TraceOption {
	return func(t *TracingHook) {
		t.system = system
	}
}

// WithTracerProvider sets a custom TracerProvider for tracing.
func WithTracerProvider(provider trace.TracerProvider) TraceOption {
	return func(t *TracingHook) {
		t.tracer = provider.Tracer(traceScope)
	}
}

// NewTracingHook constructs a tracing hook.
func NewTracingHook(opts ...TraceOption) *TracingHook {
	t := &TracingHook{
		system: defaultSystem,
		tracer: otel.GetTracerProvider().Tracer(traceScope),
		turns:  make(map[turnKey]trace.Span),
		models: make(map[*model.Request]trace.Span),
		tools:  make(map[toolKey]trace.Span),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *TracingHook) BeforeTurn(ctx context.Context, turn *hook.Turn) error {
	if turn == nil {
		return nil
	}
	_, span := t.tracer.Start(ctx, fmt.Sprintf("invoke_agent %s", turn.AgentName))
	span.SetAttributes(
		semconv.GenAIOperationNameInvokeAgent,
		semconv.GenAISystemKey.String(t.system),
		semconv.GenAIAgentName(turn.AgentName),
		attribute.Int("blades.turn", turn.Turn),
	)
	t.mu.Lock()
	t.turns[turnKey{agent: turn.AgentName, turn: turn.Turn}] = span
	t.mu.Unlock()
	return nil
}

func (t *TracingHook) AfterTurn(_ context.Context, turn *hook.Turn, summary *hook.TurnSummary, err error) error {
	if turn == nil {
		return nil
	}
	span := t.takeTurn(turnKey{agent: turn.AgentName, turn: turn.Turn})
	if span == nil {
		return nil
	}
	defer span.End()
	recordSpanResult(span, err)
	if summary == nil {
		return nil
	}
	if summary.StopReason != "" {
		span.SetAttributes(semconv.GenAIResponseFinishReasons(string(summary.StopReason)))
	}
	if summary.Usage != nil {
		setUsage(span, summary.Usage)
	}
	return nil
}

func (t *TracingHook) BeforeModel(ctx context.Context, req *model.Request) error {
	if req == nil {
		return nil
	}
	_, span := t.tracer.Start(ctx, "generate_content")
	span.SetAttributes(
		semconv.GenAIOperationNameGenerateContent,
		semconv.GenAISystemKey.String(t.system),
	)
	if req.Model != "" {
		span.SetAttributes(semconv.GenAIRequestModel(req.Model))
	}
	t.mu.Lock()
	t.models[req] = span
	t.mu.Unlock()
	return nil
}

func (t *TracingHook) AfterModel(_ context.Context, req *model.Request, resp *model.Response, err error) error {
	if req == nil {
		return nil
	}
	span := t.takeModel(req)
	if span == nil {
		return nil
	}
	defer span.End()
	recordSpanResult(span, err)
	if resp == nil {
		return nil
	}
	if resp.StopReason != "" {
		span.SetAttributes(semconv.GenAIResponseFinishReasons(string(resp.StopReason)))
	}
	setUsage(span, &resp.Usage)
	return nil
}

func (t *TracingHook) BeforeTool(ctx context.Context, call *hook.ToolCall) error {
	if call == nil {
		return nil
	}
	name, description := toolAttrs(call.Tool)
	_, span := t.tracer.Start(ctx, fmt.Sprintf("execute_tool %s", name))
	span.SetAttributes(
		semconv.GenAIOperationNameExecuteTool,
		semconv.GenAISystemKey.String(t.system),
		semconv.GenAIToolName(name),
		attribute.Int("blades.turn", call.Turn),
	)
	if call.ID != "" {
		span.SetAttributes(semconv.GenAIToolCallID(call.ID))
	}
	if description != "" {
		span.SetAttributes(semconv.GenAIToolDescription(description))
	}
	t.mu.Lock()
	t.tools[toolKey{agent: call.AgentName, turn: call.Turn, id: call.ID, name: name}] = span
	t.mu.Unlock()
	return nil
}

func (t *TracingHook) AfterTool(_ context.Context, call *hook.ToolCall, _ *tools.Result, err error) error {
	if call == nil {
		return nil
	}
	name, _ := toolAttrs(call.Tool)
	span := t.takeTool(toolKey{agent: call.AgentName, turn: call.Turn, id: call.ID, name: name})
	if span == nil {
		return nil
	}
	defer span.End()
	recordSpanResult(span, err)
	return nil
}

func (t *TracingHook) takeTurn(key turnKey) trace.Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := t.turns[key]
	delete(t.turns, key)
	return span
}

func (t *TracingHook) takeModel(req *model.Request) trace.Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := t.models[req]
	delete(t.models, req)
	return span
}

func (t *TracingHook) takeTool(key toolKey) trace.Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := t.tools[key]
	delete(t.tools, key)
	return span
}

func toolAttrs(tool tools.Tool) (string, string) {
	if tool == nil {
		return "", ""
	}
	spec := tool.Spec()
	return spec.Name, spec.Description
}

func recordSpanResult(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetStatus(codes.Ok, codes.Ok.String())
}

func setUsage(span trace.Span, usage *model.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageInputTokens(int(usage.InputTokens)))
	}
	if usage.OutputTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageOutputTokens(int(usage.OutputTokens)))
	}
}
