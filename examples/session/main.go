package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/contrib/s3"
)

func main() {
	agent := blades.NewAgent(
		"History Tutor",
		blades.WithModel("qwen-plus"),
		blades.WithInstructions("You are a knowledgeable history tutor. Provide detailed and accurate information on historical events."),
		blades.WithProvider(openai.NewChatProvider()),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Can you tell me about the causes of World War II?"),
	)
	// Create a new session
	session := blades.NewSession()
	ctx := blades.NewSessionContext(context.Background(), session)
	// Run the agent
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	// Save session to S3
	sessionStore, err := s3.NewSessionStore("blades", aws.Config{}) // TODO: add your AWS config here
	if err != nil {
		log.Fatal(err)
	}
	if err := sessionStore.Save(ctx, session); err != nil {
		log.Fatal(err)
	}
	log.Println(result.Text())
}
