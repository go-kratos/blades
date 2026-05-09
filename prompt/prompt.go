package prompt

import (
	"context"

	"github.com/go-kratos/blades/content"
)

// Builder constructs the system prompt parts for a model request.
type Builder interface {
	Build(ctx context.Context) ([]content.Part, error)
}

// Section is a function that produces prompt parts.
type Section func(ctx context.Context) ([]content.Part, error)

// New creates a Builder from ordered sections.
func New(sections ...Section) Builder {
	return &builder{sections: sections}
}

type builder struct {
	sections []Section
}

func (b *builder) Build(ctx context.Context) ([]content.Part, error) {
	var parts []content.Part
	for _, s := range b.sections {
		p, err := s(ctx)
		if err != nil {
			return nil, err
		}
		if len(p) > 0 {
			parts = append(parts, p...)
		}
	}
	return parts, nil
}

// SystemText extracts text from parts and joins them as a single system string.
func SystemText(parts []content.Part) string {
	var text string
	for _, p := range parts {
		if t, ok := p.(content.Text); ok {
			if text != "" {
				text += "\n\n"
			}
			text += t.Text
		}
	}
	return text
}
