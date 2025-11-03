package evaluate

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades"
	"github.com/google/jsonschema-go/jsonschema"
)

// Criteria evaluates the relevancy of LLM responses.
type Criteria struct {
	agent *blades.Agent
}

// NewCriteria creates a new Criteria evaluator.
func NewCriteria(name string, opts ...blades.Option) (*Criteria, error) {
	schema, err := jsonschema.For[Evaluation](nil)
	if err != nil {
		return nil, err
	}
	agent := blades.NewAgent(
		name,
		append(opts, blades.WithOutputSchema(schema))...,
	)
	return &Criteria{agent: agent}, nil
}

// Evaluate evaluates the relevancy of the LLM response.
func (r *Criteria) Evaluate(ctx context.Context, prompt *blades.Prompt) (*Evaluation, error) {
	output, err := r.agent.Run(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var evaluation Evaluation
	if err := json.Unmarshal([]byte(output.Text()), &evaluation); err != nil {
		return nil, err
	}
	return &evaluation, nil
}
