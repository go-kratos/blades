package blades

import (
	"fmt"
	"strings"
	"text/template"
)

// NewTemplateMessage creates a single Message by rendering the provided template string with the given variables.
func NewTemplateMessage(role Role, tmpl string, vars map[string]any) (*Message, error) {
	var buf strings.Builder
	if len(vars) > 0 {
		t, err := template.New("message").Option("missingkey=error").Parse(tmpl)
		if err != nil {
			return nil, err
		}
		if err := t.Execute(&buf, vars); err != nil {
			return nil, err
		}
	} else {
		buf.WriteString(tmpl)
	}
	switch role {
	case RoleUser:
		return UserMessage(buf.String()), nil
	case RoleSystem:
		return SystemMessage(buf.String()), nil
	case RoleAssistant:
		return AssistantMessage(buf.String()), nil
	default:
		return nil, fmt.Errorf("unknown role: %s", role)
	}
}
