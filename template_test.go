package blades

import "testing"

func TestTemplateMessage(t *testing.T) {
	tmpl := "Hello, {{.Name}}! Welcome to {{.Place}}."
	data := map[string]any{
		"Name":  "Alice",
		"Place": "Wonderland",
	}

	expected := "Hello, Alice! Welcome to Wonderland."
	result, err := NewPromptTemplate().System(tmpl, data).Build()
	if err != nil {
		t.Fatalf("TemplateMessage returned an error: %v", err)
	}
	if result.Latest().Text() != expected {
		t.Errorf("TemplateMessage = %q; want %q", result, expected)
	}
}

func TestTemplateMessageEmpty(t *testing.T) {
	tmpl := "Hello Alice"
	expected := "Hello Alice"
	result, err := NewPromptTemplate().System(tmpl).Build()
	if err != nil {
		t.Fatalf("TemplateMessage returned an error: %v", err)
	}
	if result.Latest().Text() != expected {
		t.Errorf("TemplateMessage = %q; want %q", result, expected)
	}
}

func TestTemplateMessageConflict(t *testing.T) {
	tmpl := "Hello Alice {{.name}}"
	expected := "Hello Alice {{.name}}"
	result, err := NewPromptTemplate().System(tmpl).Build()
	if err != nil {
		t.Fatalf("TemplateMessage returned an error: %v", err)
	}
	if result.Latest().Text() != expected {
		t.Errorf("TemplateMessage = %q; want %q", result, expected)
	}
}
