package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/summary"
	"github.com/go-kratos/blades/contrib/openai"
)

// This example demonstrates the summary ContextCompressor, which compresses old
// conversation history into a rolling LLM-generated summary whenever the
// token budget is exceeded. The most recent messages are always kept verbatim,
// while earlier ones are folded into a concise summary, giving the model a
// compact representation of the full conversation history.
func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	// Use the same model as both the main agent and the summarizer.
	// In production you might use a cheaper/faster model for summarization.
	compressor := summary.NewContextCompressor(
		model,
		// Trigger compression once the context exceeds ~500 tokens.
		summary.WithMaxTokens(500),
		// Always keep the 3 most recent messages verbatim.
		summary.WithKeepRecent(3),
		// Compress up to 5 messages per summarization pass.
		summary.WithBatchSize(5),
	)

	agent, err := blades.NewAgent(
		"SummaryDemo",
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
		"Tell me a one-sentence fact about the Sun.",
		"Tell me a one-sentence fact about the Moon.",
		"Tell me a one-sentence fact about Mars.",
		"Tell me a one-sentence fact about Jupiter.",
		"Tell me a one-sentence fact about Saturn.",
		"Tell me a one-sentence fact about Venus.",
		// By now older messages may have been summarised to stay within budget.
		"Which planets have I asked about so far?",
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
