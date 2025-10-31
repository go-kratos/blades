package evaluate

import (
	"context"

	"github.com/go-kratos/blades"
)

// Evaluation represents the result of evaluating an LLM response.
type Evaluation struct {
	Pass     bool    `json:"pass" jsonschema:"Indicates whether the response satisfies the evaluation criteria."`
	Score    float64 `json:"score" jsonschema:"LLM-judged similarity to the expected response; score in [0,1], higher is better."`
	Feedback string  `json:"feedback" jsonschema:"Detailed feedback on the LLM response."`
}

// Evaluator defines the interface for evaluating LLM responses.
type Evaluator interface {
	Evaluate(context.Context, *blades.Prompt) (*Evaluation, error)
}
