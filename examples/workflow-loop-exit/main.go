package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/tools"
)

// workflow-loop-exit demonstrates using tools.ExitTool to let the reviewer
// LLM decide when to stop the write/review loop, instead of hard-coding a
// string-matching condition.
//
// The reviewer agent has the exit tool registered. When it is satisfied with
// the draft it calls exit({"reason":"..."}) to complete normally, or
// exit({"reason":"...","escalate":true}) to escalate to a human reviewer.
func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	writerAgent, err := blades.NewAgent(
		"WriterAgent",
		blades.WithModel(model),
		blades.WithInstruction(`Write a short paragraph about climate change.
{{if .feedback}}
Revise your previous draft based on this feedback:
{{.feedback}}

Previous draft:
{{.draft}}
{{end}}`),
		blades.WithOutputKey("draft"),
	)
	if err != nil {
		log.Fatal(err)
	}

	exitTool := tools.NewExitTool()

	reviewerAgent, err := blades.NewAgent(
		"ReviewerAgent",
		blades.WithModel(model),
		blades.WithInstruction(`Review the following draft and provide concise feedback.

Draft:
{{.draft}}

If the draft is satisfactory, call the exit tool with a brief reason.
If the draft needs major rework that requires a human, call exit with escalate=true.
Otherwise, provide your feedback as plain text so the writer can revise.`),
		blades.WithTools(exitTool),
		blades.WithOutputKey("feedback"),
	)
	if err != nil {
		log.Fatal(err)
	}

	loopAgent := flow.NewLoopAgent(flow.LoopConfig{
		Name:          "WritingReviewFlow",
		Description:   "Write and review until the reviewer signals exit via tool call.",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{writerAgent, reviewerAgent},
	})

	runner := blades.NewRunner(loopAgent)
	stream := runner.RunStream(context.Background(), blades.UserMessage("Write about climate change."))
	for message, err := range stream {
		if errors.Is(err, blades.ErrLoopEscalated) {
			log.Println("Loop escalated — requires human review.")
			return
		}
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[%s] %s\n", message.Author, message.Text())
	}
}
