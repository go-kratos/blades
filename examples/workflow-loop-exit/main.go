package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/window"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/tools"
)

// workflow-loop-exit demonstrates fully autonomous write-review cycles using
// tools.ExitTool. The user provides a single input; the loop runs until the
// reviewer is satisfied.
//
// Context management:
//   - Cross-iteration history is accumulated by the loop and injected into
//     each sub-agent's invocation.History automatically.
//   - blades.WithContextManager on the Runner trims the context window before
//     every model call, preventing unbounded growth over many iterations.
func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	writerAgent, err := blades.NewAgent(
		"WriterAgent",
		blades.WithModel(model),
		blades.WithInstruction(`You are a skilled writer.
Write a short paragraph on the topic given by the user.
If conversation history contains reviewer feedback, revise accordingly.`),
	)
	if err != nil {
		log.Fatal(err)
	}

	exitTool := tools.NewExitTool()

	reviewerAgent, err := blades.NewAgent(
		"ReviewerAgent",
		blades.WithModel(model),
		blades.WithInstruction(`You are a critical editor.
Review the most recent draft in the conversation history and decide:
- If it meets a high standard, call the exit tool with a brief reason.
- If it needs minor improvement, provide concise feedback as plain text.
- If it has fundamental problems requiring human judgement, call exit with escalate=true.`),
		blades.WithTools(exitTool),
	)
	if err != nil {
		log.Fatal(err)
	}

	loopAgent := flow.NewLoopAgent(flow.LoopConfig{
		Name:          "WritingReviewFlow",
		Description:   "Autonomous write-review loop driven by ExitTool.",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{writerAgent, reviewerAgent},
	})

	// WithContextManager is configured once on the Runner and applies to all
	// agents in the pipeline. Keep at most 8 messages (~4 write-review pairs).
	runner := blades.NewRunner(loopAgent,
		blades.WithContextManager(window.NewContextManager(window.WithMaxMessages(8))),
	)
	stream := runner.RunStream(context.Background(), blades.UserMessage("Write about the impact of climate change on coastal cities."))
	for message, err := range stream {
		if errors.Is(err, blades.ErrLoopEscalated) {
			log.Println("Escalated — requires human review.")
			return
		}
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("[%s] %s\n", message.Author, message.Text())
	}
}
