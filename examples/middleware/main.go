package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

// Guardrails is a middleware that adds guardrails to the prompt.
type Guardrails struct {
	next blades.Runnable
}

// Name returns the name of the middleware.
func (m *Guardrails) Name() string { return "guardrails" }

// Run processes the prompt and adds guardrails before passing it to the next runnable.
func (m *Guardrails) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt")
	return m.next.Run(ctx, prompt, opts...)
}

// RunStream processes the prompt in a streaming manner and adds guardrails before passing it to the next runnable.
func (m *Guardrails) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt (streaming)")
	return m.next.RunStream(ctx, prompt, opts...)
}

func newGuardrails(next blades.Runnable) blades.Runnable {
	return &Guardrails{next}
}

func defaultMiddleware() blades.Middleware {
	return blades.ChainMiddlewares(
		newGuardrails,
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
