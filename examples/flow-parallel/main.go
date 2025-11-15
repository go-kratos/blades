package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	model := openai.NewModel("deepseek-chat")
	writerAgent, err := blades.NewAgent(
		"writerAgent",
		blades.WithModel(model),
		blades.WithInstructions("Draft a short paragraph on climate change."),
	)
	if err != nil {
		log.Fatal(err)
	}
	editorAgent1, err := blades.NewAgent(
		"editorAgent1",
		blades.WithModel(model),
		blades.WithInstructions("Edit the paragraph for grammar."),
	)
	if err != nil {
		log.Fatal(err)
	}
	editorAgent2, err := blades.NewAgent(
		"editorAgent1",
		blades.WithModel(model),
		blades.WithInstructions("Edit the paragraph for style."),
	)
	if err != nil {
		log.Fatal(err)
	}
	reviewerAgent, err := blades.NewAgent(
		"finalReviewerAgent",
		blades.WithModel(model),
		blades.WithInstructions(`paragraph: {{.writerAgent}}
		grammar: {{.editorAgent1}}
		style: {{.editorAgent2}}
		Consolidate the grammar and style edits into a final version.`),
	)
	if err != nil {
		log.Fatal(err)
	}
	parallelAgent := flow.NewParallelAgent(flow.ParallelConfig{
		Name:        "EditorParallelAgent",
		Description: "Edits the drafted paragraph in parallel for grammar and style.",
		SubAgents: []blades.Agent{
			writerAgent,
			editorAgent1,
			editorAgent2,
			reviewerAgent,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	session := blades.NewSession()
	runner := blades.NewRunner(parallelAgent, blades.WithSession(session))
	stream := runner.RunStream(context.Background(), input)
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		if message.Status != blades.StatusCompleted {
			continue
		}
		session.PutState(message.Author, message.Text())
		// Print the final consolidated paragraph
		log.Println(message.Author, message.Text())
	}
}
