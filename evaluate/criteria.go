package evaluate

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades"
	"github.com/google/jsonschema-go/jsonschema"
)

// Criteria evaluates the relevancy of LLM responses.
type Criteria struct {
	runner *blades.Runner
}

// NewCriteria creates a new Criteria evaluator.
func NewCriteria(name string, opts ...blades.AgentOption) (*Criteria, error) {
	schema, err := jsonschema.For[Evaluation](nil)
	if err != nil {
		return nil, err
	}
	agent := blades.NewAgent(
		name,
		append(opts, blades.WithOutputSchema(schema))...,
	)
	runner := blades.NewRunner(agent)
	return &Criteria{runner: runner}, nil
}

// Evaluate evaluates the relevancy of the LLM response.
func (r *Criteria) Evaluate(ctx context.Context, message *blades.Message, opts ...blades.ModelOption) (*Evaluation, error) {
	output, err := r.runner.Run(ctx, message, opts...)
	if err != nil {
		return nil, err
	}
	var evaluation Evaluation
	if err := json.Unmarshal([]byte(output.Text()), &evaluation); err != nil {
		return nil, err
	}
	return &evaluation, nil
}
