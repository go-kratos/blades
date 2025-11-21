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
		"writerAgent",
		blades.WithModel(model),
		blades.WithInstruction("Draft a short paragraph on climate change."),
		blades.WithOutputKey("draft"),
	)
	if err != nil {
		log.Fatal(err)
	}
	editorAgent1, err := blades.NewAgent(
		"editorAgent1",
		blades.WithModel(model),
		blades.WithInstruction(`Edit the paragraph for grammar.
			**Paragraph:**
			{{.draft}}
		`),
		blades.WithOutputKey("grammar_edit"),
	)
	if err != nil {
		log.Fatal(err)
	}
	editorAgent2, err := blades.NewAgent(
		"editorAgent1",
		blades.WithModel(model),
		blades.WithInstruction(`Edit the paragraph for style.
			**Paragraph:**
			{{.draft}}
		`),
		blades.WithOutputKey("style_edit"),
	)
	if err != nil {
		log.Fatal(err)
	}
	reviewerAgent, err := blades.NewAgent(
		"finalReviewerAgent",
		blades.WithModel(model),
		blades.WithInstruction(`Consolidate the grammar and style edits into a final version.
			**Draft:**
			{{.draft}}

			**Grammar Edit:**
			{{.grammar_edit}}

			**Style Edit:**
			{{.style_edit}}
		`),
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
	// Run the sequential agent with streaming
	ctx := context.Background()
	runner := blades.NewRunner(sequentialAgent)
	stream := runner.RunStream(ctx, input, blades.WithSession(session))
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
