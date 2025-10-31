package evaluate

import (
	"context"

	"github.com/go-kratos/blades"
)

// Evaluation represents a single evaluation case.
type Evaluation struct {
	ID     string          `json:"id"`     // unique identifier for the evaluation case
	Input  *blades.Message `json:"input"`  // input prompt to the LLM
	Output *blades.Message `json:"output"` // LLM response to be evaluated
}

// Result represents the outcome of an evaluation.
type Result struct {
	Pass     bool    `json:"pass" jsonschema:"Indicates whether the response satisfies the evaluation criteria."`
	Score    float64 `json:"score" jsonschema:"LLM-judged similarity to the expected response; score in [0,1], higher is better."`
	Feedback string  `json:"feedback" jsonschema:"Detailed feedback on the LLM response."`
}

// Evaluator defines the interface for evaluating LLM responses.
type Evaluator interface {
	Evaluate(context.Context, *Evaluation) (*Result, error)
}
