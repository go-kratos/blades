package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/middleware"
	"github.com/go-kratos/blades/stream"
)

func main() {
	model := openai.NewModel("deepseek-chat")
	ra, err := blades.NewAgent(
		"retry-agent",
		blades.WithModel(model),
		blades.WithMiddleware(middleware.Retry(2), mockErr()),
	)
	if err != nil {
		panic(err)
	}
	msg, err := blades.NewRunner(ra).Run(context.Background(), blades.UserMessage("What is the capital of France?"))
	if err != nil {
		panic(err)
	}
	fmt.Println(msg)
}

func mockErr() blades.Middleware {
	attempts := 0
	// Return the mock error middleware
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
