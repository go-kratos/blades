package evaluate

import (
	"context"

	"github.com/go-kratos/blades"
)

// Feedback provides structured feedback on the evaluation results.
type Feedback struct {
	Summary     string   `json:"summary" jsonschema:"Short summary of evaluation results."`
	Details     string   `json:"details" jsonschema:"Detailed explanation of strengths, weaknesses, and reasoning."`
	Suggestions []string `json:"suggestions" jsonschema:"List of recommended improvements or fixes."`
}

// Evaluation represents the result of evaluating an LLM response.
type Evaluation struct {
	Pass     bool      `json:"pass" jsonschema:"Indicates whether the response satisfies the evaluation criteria."`
	Score    float64   `json:"score" jsonschema:"LLM-judged similarity to the expected response; score in [0,1], higher is better."`
	Feedback *Feedback `json:"feedback" jsonschema:"Structured feedback on the evaluation results."`
}

// Evaluator defines the interface for evaluating LLM responses.
type Evaluator interface {
	Evaluate(context.Context, *blades.Prompt) (*Evaluation, error)
}
