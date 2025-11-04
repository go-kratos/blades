package blades

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"text/template"
)

// templateText holds the data for a single message template.
type templateText struct {
	// role indicates which type of message this template produces
	role Role
	// template is the raw Go text/template string
	template string
	// vars holds the data used to render the template
	vars map[string]any
	// name is an identifier for this template instance (useful for debugging)
	name string
}

// PromptTemplate builds a Prompt from formatted system and user templates.
// It supports fluent chaining, for example:
//
//	prompt := NewPromptTemplate().User(userTmpl, params).System(sysTmpl, params).Build()
//
// Exported aliases (User/System/Build) are also provided for external packages.
type PromptTemplate struct {
	tmpls []*templateText
}

// NewPromptTemplate creates a new PromptTemplate builder.
func NewPromptTemplate() *PromptTemplate {
	return &PromptTemplate{}
}

// User appends a user message rendered from the provided template and one or more parameter maps.
// Each map is merged in order; later maps override keys from earlier maps. The merged map is accessible in the template (e.g., {{.name}}).
func (p *PromptTemplate) User(tmpl string, vars ...map[string]any) *PromptTemplate {
	if tmpl == "" {
		return p
	}
	t := &templateText{
		role:     RoleUser,
		template: tmpl,
		vars:     make(map[string]any),
		name:     fmt.Sprintf("user-%d", len(p.tmpls)),
	}
	for _, m := range vars {
		for k, v := range m {
			t.vars[k] = v
		}
	}
	p.tmpls = append(p.tmpls, t)
	return p
}

// System appends a system message rendered from the provided template and one or more parameter maps.
// Each map is merged in order; later maps override keys from earlier maps. The merged map is accessible in the template (e.g., {{.name}}).
func (p *PromptTemplate) System(tmpl string, vars ...map[string]any) *PromptTemplate {
	if tmpl == "" {
		return p
	}
	t := &templateText{
		role:     RoleSystem,
		template: tmpl,
		vars:     make(map[string]any),
		name:     fmt.Sprintf("system-%d", len(p.tmpls)),
	}
	for _, m := range vars {
		for k, v := range m {
			t.vars[k] = v
		}
	}
	p.tmpls = append(p.tmpls, t)
	return p
}

// Build finalizes and returns the constructed Prompt.
func (p *PromptTemplate) Build() (*Prompt, error) {
	messages := make([]*Message, 0, len(p.tmpls))
	for _, tmpl := range p.tmpls {
		message, err := NewTemplateMessage(tmpl.role, tmpl.template, tmpl.vars)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return NewPrompt(messages...), nil
}

// BuildContext finalizes and returns the constructed Prompt using the provided context to fill in session state variables.
func (p *PromptTemplate) BuildContext(ctx context.Context) (*Prompt, error) {
	session, ok := FromSessionContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no session found in context")
	}
	messages := make([]*Message, 0, len(p.tmpls))
	for _, tmpl := range p.tmpls {
		state := maps.Clone(map[string]any(session.State()))
		for k, v := range tmpl.vars {
			state[k] = v
		}
		message, err := NewTemplateMessage(tmpl.role, tmpl.template, state)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return NewPrompt(messages...), nil
}

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
