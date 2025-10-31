package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/evaluate"
)

func main() {
	qa := map[string]string{
		"What is the capital of France?":  "Paris.",
		"Convert 5 kilometers to meters.": "60 km/h.",
	}
	r, err := evaluate.NewRelevancy(
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	if err != nil {
		log.Fatal(err)
	}
	for q, a := range qa {
		result, err := r.Evaluate(context.Background(), &evaluate.Evaluation{
			Input:  blades.UserMessage(q),
			Output: blades.AssistantMessage(a),
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Println(result)
	}
}
