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
func NewCriteria(name string, opts ...blades.AgentOption) (*Criteria, error) {
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

// Evaluate evaluates the relevancy of the LLM response.
func (r *Criteria) Evaluate(ctx context.Context, message *blades.Message) (*Evaluation, error) {
	iter := r.agent.Run(ctx, blades.NewInvocation(message))
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
