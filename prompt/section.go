package prompt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/memory"
)

// Static creates a section that always returns the given parts.
func Static(parts ...content.Part) Section {
	cp := make([]content.Part, len(parts))
	copy(cp, parts)
	return func(_ context.Context) ([]content.Part, error) {
		out := make([]content.Part, len(cp))
		copy(out, cp)
		return out, nil
	}
}

// Text creates a section with a static text string.
func Text(text string) Section {
	return func(_ context.Context) ([]content.Part, error) {
		if text == "" {
			return nil, nil
		}
		return []content.Part{content.Text{Text: text}}, nil
	}
}

// Memory creates a section that recalls relevant memories at build time.
func Memory(mem memory.Memory, query func(context.Context) (memory.Query, error)) Section {
	return func(ctx context.Context) ([]content.Part, error) {
		if mem == nil {
			return nil, errors.New("prompt: memory is nil")
		}
		if query == nil {
			return nil, errors.New("prompt: memory query is nil")
		}
		q, err := query(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(q.Text) == "" && len(q.Filter) == 0 {
			return nil, nil
		}
		entries, err := mem.Recall(ctx, q)
		if err != nil {
			return nil, err
		}
		text, err := renderMemory(entries)
		if err != nil {
			return nil, err
		}
		if text == "" {
			return nil, nil
		}
		return []content.Part{content.Text{Text: text}}, nil
	}
}

func renderMemory(entries []memory.Entry) (string, error) {
	var b strings.Builder
	count := 0
	for _, entry := range entries {
		text, err := JoinText(entry.Parts)
		if err != nil {
			return "", fmt.Errorf("prompt: memory entry %q: %w", entry.ID, err)
		}
		if text == "" {
			continue
		}
		if count == 0 {
			b.WriteString("Relevant memory:")
		}
		count++
		fmt.Fprintf(&b, "\n%d. %s", count, text)
	}
	return b.String(), nil
}
