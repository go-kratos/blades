package prompt

import (
	"context"
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/memory"
)

// Static creates a section that always returns the given parts.
func Static(parts ...content.Part) Section {
	return func(_ context.Context) ([]content.Part, error) {
		return parts, nil
	}
}

// Dynamic creates a section from a function evaluated at build time.
func Dynamic(fn func(context.Context) ([]content.Part, error)) Section {
	return fn
}

// System creates a section with a static text string.
func System(text string) Section {
	return func(_ context.Context) ([]content.Part, error) {
		if text == "" {
			return nil, nil
		}
		return []content.Part{content.Text{Text: text}}, nil
	}
}

// Memory creates a section that recalls relevant memories at build time.
func Memory(mem memory.Memory, query func(context.Context) (string, error), opts ...memory.RecallOption) Section {
	return func(ctx context.Context) ([]content.Part, error) {
		q, err := query(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(q) == "" {
			return nil, nil
		}
		return mem.Recall(ctx, q, opts...)
	}
}
