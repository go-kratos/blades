package blades

import (
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

// mergeParams combines multiple param maps into one.
func (p *PromptTemplate) mergeParams(params ...map[string]any) map[string]any {
	out := make(map[string]any)
	for _, kv := range params {
		if kv == nil {
			continue
		}
		maps.Copy(out, kv)
	}
	return out
}

// User appends a user message rendered from the provided template and params.
// Params may be a map or struct accessible via Go text/template (e.g., {{.name}}).
func (p *PromptTemplate) User(tmpl string, params ...map[string]any) *PromptTemplate {
	p.tmpls = append(p.tmpls, &templateText{
		role:     RoleUser,
		template: tmpl,
		vars:     p.mergeParams(params...),
		name:     fmt.Sprintf("user-%d", len(p.tmpls)),
	})
	return p
}

// System appends a system message rendered from the provided template and params.
// Params may be a map or struct accessible via Go text/template (e.g., {{.name}}).
func (p *PromptTemplate) System(tmpl string, params ...map[string]any) *PromptTemplate {
	p.tmpls = append(p.tmpls, &templateText{
		role:     RoleSystem,
		template: tmpl,
		vars:     p.mergeParams(params...),
		name:     fmt.Sprintf("system-%d", len(p.tmpls)),
	})
	return p
}

// Build finalizes and returns the constructed Prompt.
func (p *PromptTemplate) Build() (*Prompt, error) {
	messages := make([]*Message, 0, len(p.tmpls))
	for _, tmpl := range p.tmpls {
		var buf strings.Builder
		t, err := template.New(tmpl.name).Parse(tmpl.template)
		if err != nil {
			return nil, err
		}
		if err := t.Execute(&buf, tmpl.vars); err != nil {
			return nil, err
		}
		switch tmpl.role {
		case RoleUser:
			messages = append(messages, UserMessage(buf.String()))
		case RoleSystem:
			messages = append(messages, SystemMessage(buf.String()))
		case RoleAssistant:
			messages = append(messages, AssistantMessage(buf.String()))
		default:
			return nil, fmt.Errorf("unknown role: %s", tmpl.role)
		}
	}
	return NewPrompt(messages...), nil
}
