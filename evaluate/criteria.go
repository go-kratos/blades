package evaluate

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades"
	"github.com/google/jsonschema-go/jsonschema"
)

// Criteria evaluates the relevancy of LLM responses.
type Criteria struct {
	agent blades.Agent
}

// NewCriteria creates a new Criteria evaluator.
func NewCriteria(name string, opts ...blades.AgentOption) (Evaluator, error) {
	schema, err := jsonschema.For[Evaluation](nil)
	if err != nil {
		return nil, err
	}
	agent, err := blades.NewAgent(
		name,
		append(opts, blades.WithOutputSchema(schema))...,
	)
	if err != nil {
		return nil, err
	}
	return &Criteria{agent: agent}, nil
}

// Run evaluates the LLM response against the configured criteria.
func (r *Criteria) Run(ctx context.Context, message *blades.Message) (*Evaluation, error) {
	iter := r.agent.Run(ctx, &blades.Invocation{Message: message})
	for msg, err := range iter {
		if err != nil {
			return nil, err
		}
		var evaluation Evaluation
		if err := json.Unmarshal([]byte(msg.Text()), &evaluation); err != nil {
			return nil, err
		}
		return &evaluation, nil
	}
	return nil, blades.ErrNoFinalResponse
}
