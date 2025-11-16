package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/middleware"
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
		blades.WithInstructions("Consolidate the grammar and style edits into a final version."),
		blades.WithMiddleware(middleware.ConversationBuffered(10)),
	)
	if err != nil {
		log.Fatal(err)
	}
	parallelAgent := flow.NewParallelAgent(flow.ParallelConfig{
		Name:        "EditorParallelAgent",
		Description: "Edits the drafted paragraph in parallel for grammar and style.",
		SubAgents: []blades.Agent{
			editorAgent1,
			editorAgent2,
		},
	})
	sequentialAgent := flow.NewSequentialAgent(flow.SequentialConfig{
		Name:        "WritingSequenceAgent",
		Description: "Drafts, edits, and reviews a paragraph about climate change.",
		SubAgents: []blades.Agent{
			writerAgent,
			parallelAgent,
			reviewerAgent,
		},
	})
	session := blades.NewSession()
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	runner := blades.NewRunner(sequentialAgent, blades.WithSession(session))
	stream := runner.RunStream(context.Background(), input)
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		// Only log completed messages
		if message.Status != blades.StatusCompleted {
			continue
		}
		log.Println(message.Author, message.Text())
	}
}
