package main

import (
	"context"
	"errors"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func newLogging() blades.Middleware {
	return blades.ChainMiddlewares(
		blades.Unary(func(next blades.RunHandler) blades.RunHandler {
			return func(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
				agent, ok := blades.FromContext(ctx)
				if !ok {
					return nil, errors.New("agent not found in context")
				}
				res, err := next(ctx, prompt, opts...)
				if err != nil {
					log.Printf("generate model: %s prompt: %s error: %v\n", agent.Model, prompt.String(), err)
				} else {
					log.Printf("generate model: %s prompt: %s response: %s\n", agent.Model, prompt.String(), res.Text())
				}
				return res, err
			}
		}),
		blades.Streaming(func(next blades.StreamHandler) blades.StreamHandler {
			return func(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
				agent, ok := blades.FromContext(ctx)
				if !ok {
					return nil, errors.New("agent not found in context")
				}
				stream, err := next(ctx, prompt, opts...)
				if err != nil {
					return nil, err
				}
				return blades.NewMappedStream[*blades.Generation, *blades.Generation](stream, func(m *blades.Generation) (*blades.Generation, error) {
					log.Printf("stream model: %s prompt: %s generation: %s\n", agent.Model, prompt.String(), m.Text())
					return m, nil
				}), nil
			}
		}),
	)
}

func newGuardrails() blades.Middleware {
	return blades.ChainMiddlewares(
		blades.Unary(func(next blades.RunHandler) blades.RunHandler {
			return func(ctx context.Context, p *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
				// Pre-processing: Add guardrails to the prompt
				log.Println("Applying guardrails to the prompt")
				return next(ctx, p, opts...)
			}
		}),
		blades.Streaming(func(next blades.StreamHandler) blades.StreamHandler {
			return func(ctx context.Context, p *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
				// Pre-processing: Add guardrails to the prompt
				log.Println("Applying guardrails to the prompt (streaming)")
				return next(ctx, p, opts...)
			}
		}),
	)
}

func defaultMiddleware() blades.Middleware {
	return blades.ChainMiddlewares(
		newLogging(),
		newGuardrails(),
	)
}

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMiddleware(defaultMiddleware()),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
