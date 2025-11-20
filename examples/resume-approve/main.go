package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/middleware"
)

func confirmPrompt(ctx context.Context, message *blades.Message) (bool, error) {
	session, ok := blades.FromSessionContext(ctx)
	if !ok {
		return false, fmt.Errorf("no session found in context")
	}
	state := session.State()
	return state["approved"] == "true", nil
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
		blades.WithMiddleware(
			middleware.Confirm(confirmPrompt),
		),
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
	input := blades.UserMessage("Please write a short paragraph about climate change.")
	ctx := context.Background()
	session := blades.NewSession()
	invocationID := "invocation-001"
	// First run that will pause for approval (requires confirmation before proceeding)
	runner := blades.NewRunner(sequentialAgent, blades.WithResumable(true))
	output, err := runner.Run(
		ctx,
		input,
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	if err != nil {
		log.Println(err)
	} else {
		log.Fatal("expected an error but got none")
	}
	// Resume from the previous session
	session.SetState("approved", "true")
	output, err = runner.Run(
		ctx,
		input,
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Author, output.Text())
}
