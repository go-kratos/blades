package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

var (
	input  string
	output string
)

func init() {
	flag.StringVar(&input, "input", "../../README_zh.md", "input file path")
	flag.StringVar(&output, "output", "../../README.md", "output file path")
}

func main() {
	flag.Parse()
	tr := blades.NewAgent(
		"Document translator",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("Translate the Chinese text within the given Markdown content to fluent, publication-quality English, perfectly preserving all Markdown syntax and structure, and outputting only the raw translated Markdown content."),
		blades.WithProvider(openai.NewChatProvider()),
	)
	refine := blades.NewAgent(
		"Refine Agent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("Polish the following translated Markdown text by refining its sentence structure and correcting grammatical errors to improve fluency and readability, while ensuring the original meaning and all Markdown \n  syntax remain unchanged"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	content, err := os.ReadFile(input)
	if err != nil {
		log.Fatal(err)
	}
	var (
		input  = blades.UserMessage(string(content))
		output *blades.Message
	)
	for _, agent := range []blades.Agent{tr, refine} {
		runner := blades.NewRunner(agent)
		output, err = runner.Run(context.Background(), input)
		if err != nil {
			log.Fatal(err)
		}
		input = output
	}
	if err := os.WriteFile(output.Text(), []byte(output.Text()), 0644); err != nil {
		log.Fatal(err)
	}
}
