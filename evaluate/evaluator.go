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
	Pass    bool                   `json:"pass"`    // whether the case passed the evaluation
	Score   float64                `json:"score"`   // evaluation score
	Details map[string]interface{} `json:"details"` // additional details
}

// Evaluator defines the interface for evaluating LLM responses.
type Evaluator interface {
	Evaluate(context.Context, *Evaluation) (*Result, error)
}
