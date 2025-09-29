package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

type Topic struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func main() {
	agent := blades.NewAgent(
		"Flow Agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	params := map[string]any{
		"topic":    "The Future of Artificial Intelligence",
		"audience": "General reader",
	}
	runner := flow.NewFlow[map[string]any, Topic](agent).
		WithSystemTemplate("Please summarize {{.topic}} in three key points.").
		WithUserTemplate("Respond concisely and accurately for a {{.audience}} audience.")
	result, err := runner.Run(context.Background(), params)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result)
}
