package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func newLogging() blades.Middleware {
	return func(next blades.Runner) blades.Runner {
		return blades.NewRunnerFunc(
			func(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
				res, err := next.Run(ctx, prompt, opts...)
				if err != nil {
					log.Printf("generate prompt: %s error: %v\n", prompt.String(), err)
				} else {
					log.Printf("generate prompt: %s response: %s\n", prompt.String(), res.Message.String())
				}
				return res, err
			},
			func(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
				stream, err := next.RunStream(ctx, prompt, opts...)
				if err != nil {
					return nil, err
				}
				return blades.NewMappedStream[*blades.Generation, *blades.Generation](stream, func(m *blades.Generation) (*blades.Generation, error) {
					log.Printf("stream prompt: %s generation: %s\n", prompt.String(), m.Message.String())
					return m, nil
				}), nil
			})
	}
}

func newGuardrails() blades.Middleware {
	return func(next blades.Runner) blades.Runner {
		return blades.NewRunnerFunc(
			func(ctx context.Context, p *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
				// Pre-processing: Add guardrails to the prompt
				log.Println("Applying guardrails to the prompt")
				// Call the next runner in the chain
				return next.Run(ctx, p, opts...)
			},
			func(ctx context.Context, p *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
				// Pre-processing: Add guardrails to the prompt
				log.Println("Applying guardrails to the prompt (streaming)")
				// Call the next runner in the chain
				return next.RunStream(ctx, p, opts...)
			},
		)
	}
}

func defaultMiddleware() blades.Middleware {
	return blades.ApplyMiddlewares(
		newLogging(),
		newGuardrails(),
	)
}

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewProvider()),
		blades.WithMiddleware(defaultMiddleware()),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result)
}
