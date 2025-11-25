package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/tools"
)

func createTranslatorWorkers(model blades.ModelProvider) []tools.Tool {
	spanishAgent, err := blades.NewAgent(
		"spanish_agent",
		blades.WithDescription("An english to spanish translator"),
		blades.WithInstruction("You translate the user's message to Spanish"),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	frenchAgent, err := blades.NewAgent(
		"french_agent",
		blades.WithDescription("An english to french translator"),
		blades.WithInstruction("You translate the user's message to French"),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	italianAgent, err := blades.NewAgent(
		"italian_agent",
		blades.WithDescription("An english to italian translator"),
		blades.WithInstruction("You translate the user's message to Italian"),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	return []tools.Tool{
		blades.NewAgentTool(spanishAgent),
		blades.NewAgentTool(frenchAgent),
		blades.NewAgentTool(italianAgent),
	}
}

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	translatorWorkers := createTranslatorWorkers(model)
	orchestratorAgent, err := blades.NewAgent(
		"orchestrator_agent",
		blades.WithInstruction(`You are a translation agent. You use the tools given to you to translate.
        If asked for multiple translations, you call the relevant tools in order.
        You never translate on your own, you always use the provided tools.`),
		blades.WithModel(model),
		blades.WithTools(translatorWorkers...),
	)
	if err != nil {
		log.Fatal(err)
	}
	synthesizerAgent, err := blades.NewAgent(
		"synthesizer_agent",
		blades.WithInstruction("You inspect translations, correct them if needed, and produce a final concatenated response."),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	input := blades.UserMessage("Hi! What would you like translated, and to which languages?")
	orchestratorRunner := blades.NewRunner(orchestratorAgent)
	stream := orchestratorRunner.RunStream(ctx, input)
	var results []string
	for message, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		if message.Status != blades.StatusCompleted {
			continue
		}
		results = append(results, message.Text())
		log.Printf("Orchestrator: %s", message.Text())
	}
	synthesizerRunner := blades.NewRunner(synthesizerAgent)
	output, err := synthesizerRunner.Run(ctx, blades.UserMessage(strings.Join(results, "\n")))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Final Output: %s", output.Text())
}
