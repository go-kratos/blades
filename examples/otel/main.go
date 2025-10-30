package main

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	ot "github.com/go-kratos/blades/contrib/otel"
)

func main() {
	exporter, err := stdouttrace.New()
	if err != nil {
		panic(err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("otel-demo"),
		),
	)

	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(1*time.Millisecond)),
			sdktrace.WithResource(res),
		),
	)

	agent := blades.NewAgent(
		"otel agent",
		blades.WithMiddleware(ot.Tracing()),
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)

	prompt := blades.NewPrompt(blades.UserMessage("Write a diary about spring, within 100 words"))

	msg, err := agent.Run(context.Background(), prompt)
	if err != nil {
		panic(err)
	}
	println(msg.Text())

	stream, err := agent.RunStream(context.Background(), prompt)
	if err != nil {
		panic(err)
	}

	for stream.Next() {
		m, err := stream.Current()
		if err != nil {
			panic(err)
		}
		println(m.Text())
	}

	time.Sleep(2 * time.Second) // wait for exporter to flush

	err = exporter.Shutdown(context.Background())
	if err != nil {
		panic(err)
	}
}
