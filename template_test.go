package blades

import "testing"

func TestTemplateMessage(t *testing.T) {
	tmpl := "Hello, {{.Name}}! Welcome to {{.Place}}."
	data := map[string]string{
		"Name":  "Alice",
		"Place": "Wonderland",
	}

	expected := "Hello, Alice! Welcome to Wonderland."
	result, err := NewTemplateMessage(RoleUser, tmpl, data)
	if err != nil {
		t.Fatalf("TemplateMessage returned an error: %v", err)
	}
	if result.Text() != expected {
		t.Errorf("TemplateMessage = %q; want %q", result, expected)
	}
}

func TestTemplateMessageEmpty(t *testing.T) {
	tmpl := "Hello Alice"
	expected := "Hello Alice"
	result, err := NewTemplateMessage(RoleUser, tmpl, nil)
	if err != nil {
		t.Fatalf("TemplateMessage returned an error: %v", err)
	}
	if result.Text() != expected {
		t.Errorf("TemplateMessage = %q; want %q", result, expected)
	}
}
