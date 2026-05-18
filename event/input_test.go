package event

import (
	"reflect"
	"testing"

	"github.com/go-kratos/blades/content"
)

func TestNewPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs []any
		want   []content.Part
	}{
		{
			name:   "text only",
			inputs: []any{"hello"},
			want:   []content.Part{content.Text{Text: "hello"}},
		},
		{
			name: "multimodal",
			inputs: []any{
				"look at this:",
				content.DataPart{Bytes: []byte{0xAB}, MIME: "image/png"},
			},
			want: []content.Part{
				content.Text{Text: "look at this:"},
				content.DataPart{Bytes: []byte{0xAB}, MIME: "image/png"},
			},
		},
		{
			name:   "empty",
			inputs: nil,
			want:   []content.Part{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewPrompt(tt.inputs...)
			if len(got.Parts) != len(tt.want) {
				t.Fatalf("NewPrompt() returned %d parts, want %d", len(got.Parts), len(tt.want))
			}
			for i := range got.Parts {
				if !reflect.DeepEqual(got.Parts[i], tt.want[i]) {
					t.Errorf("part[%d] = %#v, want %#v", i, got.Parts[i], tt.want[i])
				}
			}
		})
	}
}

func TestNewSteer(t *testing.T) {
	t.Parallel()

	got := NewSteer("correction", content.Text{Text: "extra"})
	want := []content.Part{
		content.Text{Text: "correction"},
		content.Text{Text: "extra"},
	}
	if len(got.Parts) != len(want) {
		t.Fatalf("NewSteer() returned %d parts, want %d", len(got.Parts), len(want))
	}
	for i := range got.Parts {
		if !reflect.DeepEqual(got.Parts[i], want[i]) {
			t.Errorf("part[%d] = %#v, want %#v", i, got.Parts[i], want[i])
		}
	}
}
