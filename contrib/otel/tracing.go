package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-kratos/blades"
)

const (
	traceScope = "blades"
)

// TraceOption defines options for tracing middleware
type TraceOption func(*tracing)

// tracing holds configuration for the agent tracing middleware
type tracing struct {
	system string // e.g., "openai", "claude", "gemini"
	tracer trace.Tracer
	next   blades.Handler
}

// WithSystem sets the AI system name for tracing, e.g., "openai", "claude", "gemini"
func WithSystem(system string) TraceOption {
	return func(t *tracing) {
		t.system = system
	}
}

// WithTracerProvider sets a custom TracerProvider for the tracing middleware
func WithTracerProvider(tr trace.TracerProvider) TraceOption {
	return func(t *tracing) {
		t.tracer = tr.Tracer(traceScope)
	}
}

// Tracing returns a middleware that adds OpenTelemetry tracing to agent invocations
func Tracing(opts ...TraceOption) blades.Middleware {
	t := &tracing{
		system: "_OTHER",
		tracer: otel.GetTracerProvider().Tracer(traceScope),
	}
	for _, o := range opts {
		o(t)
	}
	return func(next blades.Handler) blades.Handler {
		t.next = next
		return t
	}
}

func (t *tracing) Start(ctx context.Context, agent blades.Agent, invocation *blades.Invocation) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("invoke_agent %s", agent.Name()))
	mo := &blades.ModelOptions{}
	for _, opt := range invocation.ModelOptions {
		opt(mo)
	}
	var (
		model     string
		sessionID string
	)
	if invocation.Session != nil {
		sessionID = invocation.Session.ID()
	}
	if m, ok := blades.FromModelContext(ctx); ok {
		model = m.Model()
	}
	span.SetAttributes(
		semconv.GenAIOperationNameInvokeAgent,
		semconv.GenAISystemKey.String(t.system),
		semconv.GenAIAgentName(agent.Name()),
		semconv.GenAIAgentDescription(agent.Description()),
		semconv.GenAIRequestModel(model),
		semconv.GenAIRequestTopP(mo.TopP),
		semconv.GenAIRequestSeed(int(mo.Seed)),
		semconv.GenAIRequestTemperature(mo.Temperature),
		semconv.GenAIRequestStopSequences(mo.StopSequences...),
		semconv.GenAIRequestPresencePenalty(mo.PresencePenalty),
		semconv.GenAIRequestFrequencyPenalty(mo.FrequencyPenalty),
		semconv.GenAIConversationID(sessionID),
	)
	return ctx, span
}

// Handle processes the prompt in a streaming manner and adds OpenTelemetry tracing to the invocation before passing it to the next agent.
func (t *tracing) Handle(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	agent, ok := blades.FromAgentContext(ctx)
	if !ok {
		return t.next.Handle(ctx, invocation)
	}
	return func(yield func(*blades.Message, error) bool) {
		var (
			err     error
			message *blades.Message
		)
		ctx, span := t.Start(ctx, agent, invocation)
		streaming := t.next.Handle(ctx, invocation)
		for message, err = range streaming {
			if err != nil {
				yield(nil, err)
				break
			}
			if !yield(message, nil) {
				break
			}
		}
		t.End(span, message, err)
	}
}

func (t *tracing) End(span trace.Span, msg *blades.Message, err error) {
	defer span.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, codes.Ok.String())
	}
	if msg == nil {
		return
	}
	if msg.FinishReason != "" {
		span.SetAttributes(semconv.GenAIResponseFinishReasons(msg.FinishReason))
	}
	if msg.TokenUsage.PromptTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageInputTokens(int(msg.TokenUsage.PromptTokens)))
	}
	if msg.TokenUsage.CompletionTokens > 0 {
		span.SetAttributes(semconv.GenAIUsageOutputTokens(int(msg.TokenUsage.CompletionTokens)))
	}
}
