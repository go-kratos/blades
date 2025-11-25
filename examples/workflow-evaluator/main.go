package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/evaluate"
)

func buildPrompt(topic, content, feedback string) *blades.Message {
	return blades.UserMessage(fmt.Sprintf(
		"topic: %s\n**content**\n%s\n**feedback**\n%s",
		topic,
		content,
		feedback,
	))
}

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	generator, err := blades.NewAgent(
		"story_outline_generator",
		blades.WithModel(model),
		blades.WithInstruction(`You generate a very short story outline based on the user's input.
		If there is any feedback provided, use it to improve the outline.`),
	)
	if err != nil {
		log.Fatal(err)
	}
	evaluator, err := evaluate.NewCriteria("story_evaluator",
		blades.WithModel(model),
		blades.WithInstruction(`You evaluate a story outline and decide if it's good enough.
		If it's not good enough, you provide feedback on what needs to be improved.
		You can give it a pass if the story outline is good enough - do not go for perfection`),
	)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	topic := "Generate a story outline about a brave knight who saves a village from a dragon."
	input := blades.UserMessage(topic)
	runner := blades.NewRunner(generator)
	var output *blades.Message
	for i := 0; i < 3; i++ {
		output, err = runner.Run(ctx, input)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(output.Text())
		evaluation, err := evaluator.Run(ctx, output)
		if err != nil {
			log.Fatal(err)
		}
		if evaluation.Pass {
			break
		}
		if evaluation.Feedback != nil {
			input = buildPrompt(
				topic,
				output.Text(),
				strings.Join(evaluation.Feedback.Suggestions, "\n"),
			)
		}
	}
	log.Println("Final Output:", output.Text())
}
