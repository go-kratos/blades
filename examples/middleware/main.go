package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/stream"
)

// Guardrails is a middleware that adds guardrails to the prompt.
type Guardrails struct {
	next blades.Runnable
}

// Run processes the prompt and adds guardrails before passing it to the next runnable.
func (m *Guardrails) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt")
	return m.next.Run(ctx, prompt, opts...)
}

// RunStream processes the prompt in a streaming manner and adds guardrails before passing it to the next runnable.
func (m *Guardrails) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) stream.Streamable[*blades.Message] {
	// Pre-processing: Add guardrails to the prompt
	log.Println("Applying guardrails to the prompt (streaming)")
	return func(yield func(*blades.Message, error) bool) {
		streaming := m.next.RunStream(ctx, prompt, opts...)
		for msg, err := range streaming {
			log.Println("Streaming with guardrails applied:", msg.Text())
			yield(msg, err)
		}
	}
}

func newGuardrails(next blades.Runnable) blades.Runnable {
	return &Guardrails{next}
}

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMiddleware(newGuardrails),
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
