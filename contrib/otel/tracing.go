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
	next   blades.Runnable
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

	pipe := blades.NewStreamPipe[*blades.Message]()
	pipe.Go(func() error {
		stream, err := t.next.RunStream(ctx, prompt, opts...)
		if err != nil {
			t.end(span, nil, err)
			return err
		}
		var message *blades.Message
		for stream.Next() {
			message, err = stream.Current()
			if err != nil {
				t.end(span, nil, err)
				return err
			}
			pipe.Send(message)
		}
		t.end(span, message, nil)
		return nil
	})
	return pipe, nil
}

func (t *tracing) end(span trace.Span, msg *blades.Message, err error) {
	defer span.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, codes.Ok.String())
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
