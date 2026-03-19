package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/window"
	"github.com/go-kratos/blades/contrib/openai"
)

// This example demonstrates the window ContextCompressor, which keeps only the
// most recent messages within a configured message count or token budget.
// Older messages are silently dropped from the front of the history once the
// limit is exceeded, implementing a classic sliding-window context strategy.
func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	// Keep at most the last 4 messages and stay within a 2000-token budget.
	// Adjust these values to observe different truncation behaviour.
	compressor := window.NewContextCompressor(
		window.WithMaxMessages(4),
		window.WithMaxTokens(2000),
	)

	agent, err := blades.NewAgent(
		"WindowDemo",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant. Answer concisely."),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	session := blades.NewSession(blades.WithContextCompressor(compressor))
	runner := blades.NewRunner(agent)

	turns := []string{
		"My favourite colour is blue.",
		"My favourite animal is a dolphin.",
		"My favourite food is sushi.",
		"My favourite sport is tennis.",
		"My favourite book is Dune.",
		// By this point the first few messages have been dropped from context.
		"Can you list all the favourite things I mentioned?",
	}

	for _, input := range turns {
		log.Printf("User: %s", input)
		output, err := runner.Run(ctx, blades.UserMessage(input), blades.WithSession(session))
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Agent: %s\n", output.Text())
	}
}
