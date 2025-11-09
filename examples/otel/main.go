package main

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	middleware "github.com/go-kratos/blades/contrib/otel"
)

func main() {
	exporter, err := stdouttrace.New()
	if err != nil {
		log.Fatal(err)
	}
	resource, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("otel-demo"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(1*time.Millisecond)),
			sdktrace.WithResource(resource),
		),
	)
	// Create a blades agent with OpenTelemetry middleware
	agent := blades.NewAgent(
		"OpenTelemetry Agent",
		blades.WithMiddleware(middleware.Tracing()),
		blades.WithModel("qwen-max"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	input := blades.UserMessage("Write a diary about spring, within 100 words")
	runner := blades.NewRunner(agent)
	msg, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(msg.Text())
	// Shutdown the exporter to flush any remaining spans
	if err := exporter.Shutdown(context.Background()); err != nil {
		log.Fatal(err)
	}
}
