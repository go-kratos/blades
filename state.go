package blades

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// State holds arbitrary key-value pairs representing the state.
type State map[string]any

// Clone creates a deep copy of the State.
func (s State) Clone() State {
	return State(maps.Clone(map[string]any(s)))
}

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
