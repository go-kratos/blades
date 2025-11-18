package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/middleware"
	"github.com/go-kratos/blades/stream"
)

func mockRetry() blades.Middleware {
	attempts := 0
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
			if attempts == 0 {
				attempts++
				return stream.Error[*blades.Message](errors.New("mock error"))
			}
			return next.Handle(ctx, invocation)
		})
	}
}

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"RetryAgent",
		blades.WithModel(model),
		blades.WithMiddleware(
			mockRetry(),
			middleware.Retry(2),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	runner := blades.NewRunner(agent)
	msg, err := runner.Run(context.Background(), blades.UserMessage("What is the capital of France?"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg)
}
