package main

import (
	"context"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func buildPrompt(params map[string]any) (string, error) {
	var (
		tmpl = "Respond concisely and accurately for a {{.audience}} audience."
		buf  strings.Builder
	)
	t, err := template.New("message").Parse(tmpl)
	if err != nil {
		return "", err
	}
	if err := t.Execute(&buf, params); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func main() {
	// Initialize the agent with a template
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"Template Agent",
		blades.WithModel(model),
		blades.WithInstructions("Please summarize {{.topic}} in three key points."),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Define templates and params
	params := map[string]any{
		"topic":    "The Future of Artificial Intelligence",
		"audience": "General reader",
	}

	// Build prompt using the template builder
	// Note: Use exported methods when calling from another package.
	prompt, err := buildPrompt(params)
	if err != nil {
		log.Fatal(err)
	}
	input := blades.UserMessage(prompt)
	// Run the agent with the templated prompt
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
