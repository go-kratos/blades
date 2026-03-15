// Package main demonstrates loading and running an agent from an AgentSpec YAML file.
// The AgentSpec format is a Kubernetes-style declarative specification that supports
// context management, approval gates, observability, and lifecycle hooks.
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	otelMiddleware "github.com/go-kratos/blades/contrib/otel"
	"github.com/go-kratos/blades/recipe"
	"github.com/go-kratos/blades/tools"
)

//go:embed agent.yaml
var specFS embed.FS

func main() {
	// 1. Configure OpenTelemetry with stdout exporter for demo purposes.
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatal(err)
	}
	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceNameKey.String("agent-spec-demo")),
	)
	if err != nil {
		log.Fatal(err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background()) //nolint:errcheck

	// 2. Register models.
	modelRegistry := recipe.NewRegistry()
	modelRegistry.Register("gpt-4o", openai.NewModel("gpt-4o", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))
	modelRegistry.Register("gpt-4o-mini", openai.NewModel("gpt-4o-mini", openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	}))

	// 3. Register the get-weather tool referenced in agent.yaml.
	type weatherReq struct {
		City string `json:"city" jsonschema:"The city to get weather for"`
	}
	type weatherRes struct {
		Forecast string `json:"forecast"`
	}
	weatherTool, err := tools.NewFunc("get-weather", "Get current weather for a city",
		func(_ context.Context, req weatherReq) (weatherRes, error) {
			return weatherRes{Forecast: fmt.Sprintf("Sunny, 22°C in %s", req.City)}, nil
		})
	if err != nil {
		log.Fatal(err)
	}
	toolRegistry := recipe.NewStaticToolRegistry()
	toolRegistry.Register("get-weather", weatherTool)

	// 4. Register named hook middlewares (on_start: logging, on_error: alert).
	mwRegistry := recipe.NewStaticMiddlewareRegistry()
	mwRegistry.Register("logging", func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
			log.Printf("[hook:logging] invocation started (id=%s)", inv.ID)
			return next.Handle(ctx, inv)
		})
	})
	mwRegistry.Register("alert", func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
			return func(yield func(*blades.Message, error) bool) {
				for msg, err := range next.Handle(ctx, inv) {
					if err != nil {
						log.Printf("[hook:alert] error during invocation (id=%s): %v", inv.ID, err)
					}
					if !yield(msg, err) {
						return
					}
				}
			}
		})
	})

	// 5. Load the AgentSpec from YAML.
	spec, err := recipe.LoadAgentSpecFromFS(specFS, "agent.yaml")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Loaded AgentSpec: name=%q version=%q", spec.Name, spec.Version)

	// 6. Build the agent with all registered extensions.
	agent, err := recipe.BuildFromAgentSpec(spec,
		recipe.WithModelRegistry(modelRegistry),
		recipe.WithToolRegistry(toolRegistry),
		recipe.WithMiddlewareRegistry(mwRegistry),

		// Approval handler: prompt the user when bash or deploy tools are available on this agent.
		recipe.WithApprovalHandler(func(_ context.Context, msg *blades.Message) (bool, error) {
			preview := strings.TrimSpace(msg.Text())
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			log.Printf("[approval] Request: %s", preview)
			log.Println("[approval] Auto-approved for demo")
			return true, nil
		}),

		// Tracing factory: wire up contrib/otel using the ObservabilitySpec fields.
		recipe.WithTracingMiddlewareFactory(func(obs *recipe.ObservabilitySpec) blades.Middleware {
			if obs.Tracing == "otel" {
				opts := []otelMiddleware.TraceOption{}
				if obs.System != "" {
					opts = append(opts, otelMiddleware.WithSystem(obs.System))
				}
				return otelMiddleware.Tracing(opts...)
			}
			return nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 7. Run the agent.
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(),
		blades.UserMessage("What is the weather like in Tokyo? Please provide a brief code review tip too."),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("[%s] %s", agent.Name(), output.Text())
}
