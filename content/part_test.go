package content

import (
	"reflect"
	"testing"
)

func TestNewParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs []any
		want   []Part
	}{
		{
			name:   "empty",
			inputs: nil,
			want:   []Part{},
		},
		{
			name:   "single string",
			inputs: []any{"hello"},
			want:   []Part{Text{Text: "hello"}},
		},
		{
			name:   "single Text part",
			inputs: []any{Text{Text: "world"}},
			want:   []Part{Text{Text: "world"}},
		},
		{
			name:   "FilePart",
			inputs: []any{FilePart{URI: "s3://b/k", MIME: "application/pdf", Filename: "doc.pdf"}},
			want:   []Part{FilePart{URI: "s3://b/k", MIME: "application/pdf", Filename: "doc.pdf"}},
		},
		{
			name:   "DataPart",
			inputs: []any{DataPart{Bytes: []byte{1, 2, 3}, MIME: "image/png"}},
			want:   []Part{DataPart{Bytes: []byte{1, 2, 3}, MIME: "image/png"}},
		},
		{
			name: "mixed types",
			inputs: []any{
				"describe this:",
				DataPart{Bytes: []byte{0xFF}, MIME: "image/jpeg"},
				"and summarize",
			},
			want: []Part{
				Text{Text: "describe this:"},
				DataPart{Bytes: []byte{0xFF}, MIME: "image/jpeg"},
				Text{Text: "and summarize"},
			},
		},
		{
			name:   "invalid type skipped",
			inputs: []any{42, "valid", true},
			want:   []Part{Text{Text: "valid"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewParts(tt.inputs...)
			if len(got) != len(tt.want) {
				t.Fatalf("NewParts() returned %d parts, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if !reflect.DeepEqual(got[i], tt.want[i]) {
					t.Errorf("part[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
