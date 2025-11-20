package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/stream"
)

func mockErr() blades.Middleware {
	return func(next blades.Handler) blades.Handler {
		return blades.HandleFunc(func(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
			if !invocation.Resumable {
				return stream.Error[*blades.Message](errors.New("[ERROR] Simulated error in ReviewerAgent"))
			}
			return next.Handle(ctx, invocation)
		})
	}
}

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
		blades.WithOutputKey("review"),
		blades.WithMiddleware(mockErr()),
	)
	if err != nil {
		log.Fatal(err)
	}
	refactorAgent, err := blades.NewAgent(
		"RefactorAgent",
		blades.WithModel(model),
		blades.WithInstructions(`Refactor the draft based on the review.
			Draft: {{.draft}}
			Review: {{.review}}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	sequentialAgent := flow.NewSequentialAgent(flow.SequentialConfig{
		Name: "WritingReviewFlow",
		SubAgents: []blades.Agent{
			writerAgent,
			reviewerAgent,
			refactorAgent,
		},
	})
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	ctx := context.Background()
	session := blades.NewSession()
	invocationID := "invocation-001"
	// First run that encounters an error
	runner := blades.NewRunner(sequentialAgent)
	stream := runner.RunStream(
		ctx,
		input,
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	for message, err := range stream {
		if err != nil {
			log.Println(err)
			break
		}
		if message.Status != blades.StatusCompleted {
			continue
		}
		log.Println("first:", message.Author, message.Text())
	}
	// Resume from the previous session
	resumeRunner := blades.NewRunner(sequentialAgent, blades.WithResumable(true))
	resumeStream := resumeRunner.RunStream(
		ctx,
		input,
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	for message, err := range resumeStream {
		if err != nil {
			log.Println(err)
			break
		}
		if message.Status != blades.StatusCompleted {
			continue
		}
		log.Println("second:", message.Author, message.Text())
	}
}
