package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	writerAgent, err := blades.NewAgent(
		"WriterAgent",
		blades.WithModel(model),
		blades.WithInstructions("Draft a short paragraph on climate change."),
		blades.WithOutputKey("draft"),
	)
	if err != nil {
		log.Fatal(err)
	}
	reviewerAgent, err := blades.NewAgent(
		"ReviewerAgent",
		blades.WithModel(model),
		blades.WithInstructions(`Review the draft and suggest improvements.
			Draft: {{.draft}}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	sequentialAgent := flow.NewSequentialAgent(flow.SequentialConfig{
		Name: "WritingReviewFlow",
		SubAgents: []blades.Agent{
			writerAgent,
			reviewerAgent,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	runner := blades.NewRunner(sequentialAgent)
	stream := runner.RunStream(context.Background(), input)
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(message.Author, message.Text())
	}
}
