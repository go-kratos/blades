package main

import (
	"context"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/evaluate"
)

func buildPrompt(params map[string]any) (string, error) {
	var (
		tmpl = `You are an expert evaluator. Your task is to assess the relevancy of the LLM's response to the given input prompt.
Please follow these guidelines:
1. Understand the Input Prompt: Carefully read and comprehend the input prompt to grasp what is being asked.
2. Analyze the LLM's Response: Evaluate the response provided by the LLM in relation to the input prompt.
3. Determine Relevancy: Decide if the response directly addresses the input prompt. A relevant response should be on-topic and provide information or answers that align with the prompt's intent.
4. Scoring Criteria:
   - Pass: If the response is relevant and adequately addresses the prompt.
   - Fail: If the response is off-topic, irrelevant, or does not answer the prompt.
5. Provide Feedback: Offer constructive feedback on why the response was deemed relevant or irrelevant.
Use the above guidelines to evaluate the LLM's response.
Below are the inputs:
{
  "User prompt": {{ .Input }},
  "Agent response": {{ .Output }},
}`
		buf strings.Builder
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
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	r, err := evaluate.NewCriteria(
		"Evaluation Agent",
		blades.WithModel(model),
	)
	if err != nil {
		log.Fatal(err)
	}
	qa := map[string]string{
		"What is the capital of France?":  "Paris.",
		"Convert 5 kilometers to meters.": "60 km/h.",
	}
	for q, a := range qa {
		prompt, err := buildPrompt(map[string]any{
			"Input":  q,
			"Output": a,
		})
		if err != nil {
			log.Fatal(err)
		}
		input := blades.UserMessage(prompt)
		output, err := r.Evaluate(context.Background(), input)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Pass: %t Score: %f Feedback: %+v", output.Pass, output.Score, output.Feedback)
	}
}
