package content_test

import (
	"reflect"
	"testing"

	"github.com/go-kratos/blades/content"
)

type customPart struct {
	Value string
}

func (customPart) ContentKind() content.Kind { return "example.custom" }

func TestPartAcceptsExternalImplementations(t *testing.T) {
	t.Parallel()

	want := customPart{Value: "value"}
	parts := content.NewParts(want)
	if !reflect.DeepEqual(parts, []content.Part{want}) {
		t.Fatalf("NewParts() = %#v, want external part %#v", parts, want)
	}
	if got := parts[0].ContentKind(); got != "example.custom" {
		t.Fatalf("ContentKind() = %q, want %q", got, "example.custom")
	}
}
