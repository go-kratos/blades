package otel

import (
	"context"
	"fmt"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-kratos/blades"
)

const scope = "github.com/go-kratos/blades/contrib/otel"

type Option func(*tracing)

// tracing holds configuration for the agent tracing middleware
type tracing struct {
	system string // e.g., "openai", "claude", "gemini"
	tracer trace.Tracer
	next   blades.Runnable
}

// WithSystem sets the AI system name for tracing, e.g., "openai", "claude", "gemini"
func WithSystem(system string) Option {
	return func(t *tracing) {
		t.system = system
	}
}

// WithTracerProvider sets a custom TracerProvider for the tracing middleware
func WithTracerProvider(tr trace.TracerProvider) Option {
	return func(t *tracing) {
		t.tracer = tr.Tracer(scope)
	}
}

// Tracing returns a middleware that adds OpenTelemetry tracing to agent invocations
func Tracing(opts ...Option) blades.Middleware {
	t := &tracing{
		system: "unknown",
		tracer: otel.GetTracerProvider().Tracer(scope),
	}
	for _, o := range opts {
		o(t)
	}

	return func(next blades.Runnable) blades.Runnable {
		t.next = next
		return t
	}
}

func (t *tracing) start(ctx context.Context, ac *blades.AgentContext, opts ...blades.ModelOption) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("invoke_agent %s", ac.Name))

	mo := &blades.ModelOptions{}
	for _, opt := range opts {
		opt(mo)
	}

	span.SetAttributes(
		semconv.GenAIOperationNameInvokeAgent,
		semconv.GenAISystemKey.String(t.system),
		semconv.GenAIAgentName(ac.Name),
		semconv.GenAIAgentDescription(ac.Description),
		semconv.GenAIRequestModel(ac.Model),
		semconv.GenAIRequestSeed(int(mo.Seed)),
		semconv.GenAIRequestFrequencyPenalty(mo.FrequencyPenalty),
		semconv.GenAIRequestPresencePenalty(mo.PresencePenalty),
		semconv.GenAIRequestStopSequences(mo.StopSequences...),
		semconv.GenAIRequestTemperature(mo.Temperature),
		semconv.GenAIRequestTopP(mo.TopP),
	)

	// if a session is present, add the conversation ID attribute
	if s, ok := blades.FromSessionContext(ctx); ok {
		span.SetAttributes(
			semconv.GenAIConversationID(s.ID),
		)
	}
	return ctx, span
}

// Run processes the prompt and adds OpenTelemetry tracing to the invocation before passing it to the next runnable.
func (t *tracing) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	ac, ok := blades.FromContext(ctx)
	if !ok {
		return t.next.Run(ctx, prompt, opts...)
	}

	ctx, span := t.start(ctx, ac, opts...)

	msg, err := t.next.Run(ctx, prompt, opts...)

	t.end(span, msg, err)

	return msg, err
}

// RunStream processes the prompt in a streaming manner and adds OpenTelemetry tracing to the invocation before passing it to the next runnable.
func (t *tracing) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	ac, ok := blades.FromContext(ctx)
	if !ok {
		return t.next.RunStream(ctx, prompt, opts...)
	}

	ctx, span := t.start(ctx, ac, opts...)

	stream, err := t.next.RunStream(ctx, prompt, opts...)
	if err != nil {
		t.end(span, nil, err)
		return nil, err
	}

	return blades.NewMappedStream[*blades.Message, *blades.Message](stream, func(m *blades.Message) (*blades.Message, error) {
		_, err = stream.Current()
		if err != nil {
			t.end(span, m, err)
			return nil, err
		}

		if m.Status == blades.StatusCompleted {
			t.end(span, m, nil)
		}

		return m, nil
	}), nil
}

func (t *tracing) end(span trace.Span, msg *blades.Message, err error) {
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "OK")
	}

	if msg == nil {
		return
	}

	extractMessageAttributes(span, msg)
}

func extractMessageAttributes(span trace.Span, msg *blades.Message) {
	if v, ok := msg.Metadata["finish_reason"]; ok {
		span.SetAttributes(semconv.GenAIResponseFinishReasons(v))
	}
	if v, ok := msg.Metadata["input_tokens"]; ok {
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			span.SetAttributes(semconv.GenAIUsageInputTokens(int(num)))
		}
	}
	if v, ok := msg.Metadata["output_tokens"]; ok {
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			span.SetAttributes(semconv.GenAIUsageOutputTokens(int(num)))
		}
	}
}
