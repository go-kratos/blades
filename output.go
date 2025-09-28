package blades

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// OutputConverter is a wrapper around a Runnable runner that ensures the output conforms to a specified type T using JSON schema validation.
type OutputConverter[T any] struct {
	runner Runner
}

// NewOutput creates a new Output instance that wraps the given Runnable runner.
func NewOutputConverter[T any](runner Runner) *OutputConverter[T] {
	return &OutputConverter[T]{runner: runner}
}

// Run processes the given prompt using the wrapped runner and ensures the output conforms to type T.
func (o *OutputConverter[T]) Run(ctx context.Context, prompt *Prompt, opts ...ModelOption) (T, error) {
	var result T
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return result, err
	}
	// Convert the schema to JSON Schema format
	b, err := schema.MarshalJSON()
	if err != nil {
		return result, err
	}
	buf := strings.Builder{}
	buf.WriteString(`Your response should be in JSON format.
				Do not include any explanations, only provide a RFC8259 compliant JSON response following this format without deviation.
				Do not include markdown code blocks in your response.
				Here is the JSON Schema instance your output must adhere to:
				`)
	buf.WriteString(string(b))
	p := NewPrompt(SystemMessage(buf.String()))
	p.Messages = append(p.Messages, prompt.Messages...)
	res, err := o.runner.Run(ctx, p, opts...)
	if err != nil {
		return result, err
	}
	text := res.Text()
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "```json")
	text = strings.Trim(text, "```")
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return result, err
	}
	return result, nil
}

// RunStream processes the given prompt using the wrapped runner and returns a Streamable that yields a single output of type T.
func (o *OutputConverter[T]) RunStream(ctx context.Context, prompt *Prompt, opts ...ModelOption) (Streamer[T], error) {
	result, err := o.Run(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	stream := NewStreamPipe[T]()
	stream.Send(result)
	stream.Close()
	return stream, nil
}
