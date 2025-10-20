package blades

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-kratos/generics"
	"github.com/google/jsonschema-go/jsonschema"
)

// State holds arbitrary key-value pairs representing the state.
type State struct {
	generics.Map[string, any]
}

// MarshalJSON serializes the State to JSON.
func (s *State) MarshalJSON() ([]byte, error) { return s.Map.MarshalJSON() }

// UnmarshalJSON deserializes JSON data into the State.
func (s *State) UnmarshalJSON(data []byte) error { return s.Map.UnmarshalJSON(data) }

// StateInputHandler is a function type that processes input prompts with access to the current state.
type StateInputHandler func(ctx context.Context, input *Prompt, state *State) (*Prompt, error)

// StateOutputHandler is a function type that processes output messages with access to the current state.
type StateOutputHandler func(ctx context.Context, output *Message, state *State) (*Message, error)

// ParseMessageState parses the content of a Message according to the provided JSON schema.
func ParseMessageState(output *Message, schema *jsonschema.Schema) (any, error) {
	schemaType := schema.Type
	text := strings.TrimSpace(output.Text())
	switch schemaType {
	case "string":
		return text, nil
	case "integer":
		v, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer: %v", err)
		}
		return v, nil
	case "number":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %v", err)
		}
		return v, nil
	case "boolean":
		v, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean: %v", err)
		}
		return v, nil
	case "null":
		if text == "null" || text == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid null value")
	case "array":
		var arr []interface{}
		if err := json.Unmarshal([]byte(text), &arr); err != nil {
			return nil, fmt.Errorf("invalid array JSON: %v", err)
		}
		return arr, nil
	case "object":
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, fmt.Errorf("invalid object JSON: %v", err)
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("unsupported schema type: %s", schemaType)
	}
}
