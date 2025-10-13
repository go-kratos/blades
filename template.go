package blades

import (
	"fmt"
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
	vars any
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

// User appends a user message rendered from the provided template and params.
// Params may be a map or struct accessible via Go text/template (e.g., {{.name}}).
func (p *PromptTemplate) User(tmpl string, vars any) *PromptTemplate {
	if tmpl == "" {
		return p
	}
	p.tmpls = append(p.tmpls, &templateText{
		role:     RoleUser,
		template: tmpl,
		vars:     vars,
		name:     fmt.Sprintf("user-%d", len(p.tmpls)),
	})
	return p
}

// System appends a system message rendered from the provided template and params.
// Params may be a map or struct accessible via Go text/template (e.g., {{.name}}).
func (p *PromptTemplate) System(tmpl string, vars any) *PromptTemplate {
	if tmpl == "" {
		return p
	}
	p.tmpls = append(p.tmpls, &templateText{
		role:     RoleSystem,
		template: tmpl,
		vars:     vars,
		name:     fmt.Sprintf("system-%d", len(p.tmpls)),
	})
	return p
}

// Build finalizes and returns the constructed Prompt.
func (p *PromptTemplate) Build() (*Prompt, error) {
	messages := make([]*Message, 0, len(p.tmpls))
	for _, tmpl := range p.tmpls {
		msg, err := NewTemplateMessage(tmpl.role, tmpl.template, tmpl.vars)
		if err != nil {
			return nil, fmt.Errorf("rendering template %q: %w", tmpl.name, err)
		}
		messages = append(messages, msg)
	}
	return NewPrompt(messages...), nil
}

// NewTemplateMessage creates a single Message from a template string and variables.
func NewTemplateMessage(role Role, tmpl string, vars any) (*Message, error) {
	var buf strings.Builder
	t, err := template.New("message").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	if err := t.Execute(&buf, vars); err != nil {
		return nil, err
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
