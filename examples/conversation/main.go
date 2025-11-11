package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func Logging(next blades.Handler) blades.Handler {
	return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
		log.Println("history:", invocation.History)
		log.Println("message:", invocation.Message)
		return next.Handle(ctx, invocation)
	})
}

func main() {
	agent, err := blades.NewAgent(
		"Conversation Agent",
		blades.WithModel("deepseek-chat"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
		blades.WithMiddleware(
			blades.ConversationBuffer(5),
			Logging,
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	session := blades.NewSession()
	runner := blades.NewRunner(agent, blades.WithSession(session))
	output, err := runner.Run(context.Background(), blades.UserMessage("What is the capital of France?"))
	if err != nil {
		log.Fatal(err)
	}
	output2, err := runner.Run(context.Background(), blades.UserMessage("And what is the population?"))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
	log.Println(output2.Text())
}
