package evaluate

import (
	"context"

	"github.com/go-kratos/blades"
)

// Evaluation represents a single evaluation case.
type Evaluation struct {
	ID       string            `json:"id"`
	Messages []*blades.Message `json:"messages"`
	Response *blades.Message   `json:"response"`
}

// Metric represents the evaluation metric for a specific criterion.
type Metric struct {
	Pass  bool    `json:"pass"`
	Score float64 `json:"score"`
}

// Result represents the outcome of an evaluation.
type Result struct {
	Pass    bool                   `json:"pass"`    // whether the case passed the evaluation
	Metrics map[string]Metric      `json:"metrics"` // criterion name to metric
	Details map[string]interface{} `json:"details"` // additional details
}

// Evaluator defines the interface for evaluating LLM responses.
type Evaluator interface {
	Evaluate(context.Context, *Evaluation) (*Result, error)
}
