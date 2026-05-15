package prompt

import (
	"context"
	"fmt"
	"strings"

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
	cp := make([]Section, len(sections))
	copy(cp, sections)
	return &builder{sections: cp}
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

// JoinText extracts text from parts and joins them as a single system string.
func JoinText(parts []content.Part) (string, error) {
	var b strings.Builder
	for _, p := range parts {
		t, ok := p.(content.Text)
		if !ok {
			return "", fmt.Errorf("prompt: unsupported system prompt part %T", p)
		}
		if t.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t.Text)
	}
	return b.String(), nil
}
