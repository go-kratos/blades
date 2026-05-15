package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/memory"
)

func TestJoinText(t *testing.T) {
	t.Parallel()

	got, err := JoinText([]content.Part{
		content.Text{Text: "first"},
		content.Text{Text: ""},
		content.Text{Text: "second"},
	})
	if err != nil {
		t.Fatalf("JoinText() error = %v", err)
	}
	if got != "first\n\nsecond" {
		t.Fatalf("JoinText() = %q, want first\\n\\nsecond", got)
	}
}

func TestJoinTextRejectsNonText(t *testing.T) {
	t.Parallel()

	_, err := JoinText([]content.Part{content.DataPart{MIME: "text/plain"}})
	if err == nil {
		t.Fatal("JoinText() error = nil, want error")
	}
}

func TestNilSectionBuildsEmptyParts(t *testing.T) {
	t.Parallel()

	var section Section
	parts, err := section.Build(context.Background())
	if err != nil {
		t.Fatalf("Section.Build() error = %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("len(parts) = %d, want 0", len(parts))
	}
}

func TestTextSectionSkipsEmptyText(t *testing.T) {
	t.Parallel()

	parts, err := Text("").Build(context.Background())
	if err != nil {
		t.Fatalf("Text(\"\").Build() error = %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("len(parts) = %d, want 0", len(parts))
	}
}

func TestNewBuildsSectionsInOrder(t *testing.T) {
	t.Parallel()

	builder := New(
		Text("first"),
		Section(func(context.Context) ([]content.Part, error) {
			return []content.Part{content.Text{Text: "second"}}, nil
		}),
		nil,
		Text(""),
		Text("third"),
	)
	parts, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	text, err := JoinText(parts)
	if err != nil {
		t.Fatalf("JoinText() error = %v", err)
	}
	if text != "first\n\nsecond\n\nthird" {
		t.Fatalf("text = %q, want ordered sections", text)
	}
}

func TestNewReturnsSectionError(t *testing.T) {
	t.Parallel()

	want := errors.New("section failed")
	builder := New(
		Text("first"),
		Section(func(context.Context) ([]content.Part, error) {
			return nil, want
		}),
		Text("unreached"),
	)
	_, err := builder.Build(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Build() error = %v, want %v", err, want)
	}
}

func TestMemorySectionSkipsEmptyQuery(t *testing.T) {
	t.Parallel()

	mem := &recordingMemory{}
	parts, err := Memory(mem, func(context.Context) (memory.Query, error) {
		return memory.Query{}, nil
	})(context.Background())
	if err != nil {
		t.Fatalf("Memory() error = %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("len(parts) = %d, want 0", len(parts))
	}
	if mem.recallCount != 0 {
		t.Fatalf("recallCount = %d, want 0", mem.recallCount)
	}
}

func TestMemorySectionRendersEntries(t *testing.T) {
	t.Parallel()

	mem := &recordingMemory{entries: []memory.Entry{
		{ID: "1", Parts: []content.Part{content.Text{Text: "prefers concise answers"}}},
		{ID: "2", Parts: []content.Part{content.Text{Text: "uses Go"}}},
	}}
	parts, err := Memory(mem, func(context.Context) (memory.Query, error) {
		return memory.Query{Text: "project", Limit: 2, Filter: map[string]any{"scope": "repo"}}, nil
	})(context.Background())
	if err != nil {
		t.Fatalf("Memory() error = %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	text := parts[0].(content.Text).Text
	if text != "Relevant memory:\n1. prefers concise answers\n2. uses Go" {
		t.Fatalf("memory text = %q", text)
	}
	if mem.query.Text != "project" || mem.query.Limit != 2 || mem.query.Filter["scope"] != "repo" {
		t.Fatalf("query = %#v, want forwarded query", mem.query)
	}
}

func TestMemorySectionReturnsRecallError(t *testing.T) {
	t.Parallel()

	want := errors.New("recall failed")
	mem := &recordingMemory{err: want}
	_, err := Memory(mem, func(context.Context) (memory.Query, error) {
		return memory.Query{Text: "project"}, nil
	})(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Memory() error = %v, want %v", err, want)
	}
}

func TestMemorySectionRejectsNonTextEntry(t *testing.T) {
	t.Parallel()

	mem := &recordingMemory{entries: []memory.Entry{
		{ID: "image", Parts: []content.Part{content.DataPart{MIME: "image/png"}}},
	}}
	_, err := Memory(mem, func(context.Context) (memory.Query, error) {
		return memory.Query{Text: "project"}, nil
	})(context.Background())
	if err == nil || !strings.Contains(err.Error(), `memory entry "image"`) {
		t.Fatalf("Memory() error = %v, want memory entry context", err)
	}
}

type recordingMemory struct {
	query       memory.Query
	entries     []memory.Entry
	err         error
	recallCount int
}

func (m *recordingMemory) Recall(_ context.Context, query memory.Query) ([]memory.Entry, error) {
	m.recallCount++
	m.query = query
	return m.entries, m.err
}

func (m *recordingMemory) Remember(context.Context, memory.Entry) error {
	return nil
}

func (m *recordingMemory) Forget(context.Context, memory.Entry) error {
	return nil
}
