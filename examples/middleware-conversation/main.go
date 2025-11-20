package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/middleware"
)

func Logging(next blades.Handler) blades.Handler {
	return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		log.Println("history:", invocation.History)
		log.Println("message:", invocation.Message)
		return next.Handle(ctx, invocation)
	})
}

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"Conversation Agent",
		blades.WithModel(model),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
		blades.WithMiddleware(
			middleware.ConversationBuffered(5),
			Logging,
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	var (
		session = blades.NewSession()
		inputs  = []*blades.Message{
			blades.UserMessage("What is the capital of France?"),
			blades.UserMessage("And what is the population?"),
			blades.UserMessage("Summarize in one sentence."),
		}
	)
	for _, input := range inputs {
		ctx := context.Background()
		runner := blades.NewRunner(agent)
		output, err := runner.Run(ctx, input, blades.WithSession(session))
		if err != nil {
			log.Fatal(err)
		}
		log.Println(output.Text())
	}
}
