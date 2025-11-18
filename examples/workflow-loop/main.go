package main

import (
	"context"
	"log"
	"os"
	"strings"

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
		blades.WithInstructions(`Draft a short paragraph on climate change.
			{{if .suggestions}}	
			**Draft**
			{{.draft}}

			Here are the suggestions to consider:
			{{.suggestions}}
			{{end}}
		`),
		blades.WithOutputKey("draft"),
	)
	if err != nil {
		log.Fatal(err)
	}
	reviewerAgent, err := blades.NewAgent(
		"ReviewerAgent",
		blades.WithModel(model),
		blades.WithInstructions(`Review the draft and suggest improvements.
			If the draft is good, respond with "The draft is good".

			**Draft**
			{{.draft}}	
		`),
		blades.WithOutputKey("suggestions"),
	)
	if err != nil {
		log.Fatal(err)
	}
	loopAgent := flow.NewLoopAgent(flow.LoopConfig{
		Name:          "WritingReviewFlow",
		Description:   "An agent that loops between writing and reviewing until the draft is good.",
		MaxIterations: 3,
		Condition: func(ctx context.Context, output *blades.Message) (bool, error) {
			return !strings.Contains(output.Text(), "good"), nil
		},
		SubAgents: []blades.Agent{
			writerAgent,
			reviewerAgent,
		},
	})
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	runner := blades.NewRunner(loopAgent)
	stream := runner.RunStream(context.Background(), input)
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(message.Author, message.Text())
	}
}
