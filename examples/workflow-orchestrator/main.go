package main

import (
	"context"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/tools"
)

func createTranslatorWorkers(model blades.ModelProvider) []tools.Tool {
	spanishAgent, err := blades.NewAgent(
		"spanish_agent",
		blades.WithDescription("An English to Spanish translator"),
		blades.WithInstruction("You translate the user's message to Spanish"),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	frenchAgent, err := blades.NewAgent(
		"french_agent",
		blades.WithDescription("An English to French translator"),
		blades.WithInstruction("You translate the user's message to French"),
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	italianAgent, err := blades.NewAgent(
		"italian_agent",
		blades.WithDescription("An English to Italian translator"),
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
	input := blades.UserMessage("Please translate the following sentence to Spanish, French, and Italian: 'Hello, how are you?'")
	orchestratorRunner := blades.NewRunner(orchestratorAgent)
	stream := orchestratorRunner.RunStream(ctx, input)
	var message *blades.Message
	for message, err = range stream {
		if err != nil {
			log.Fatal(err)
		}
	}
	synthesizerRunner := blades.NewRunner(synthesizerAgent)
	output, err := synthesizerRunner.Run(ctx, blades.UserMessage(message.Text()))
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Final Output:", output.Text())
}
