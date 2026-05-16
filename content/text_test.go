package content

import "testing"

func TestTextFromParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []Part
		want  string
	}{
		{
			name: "concatenates text parts",
			parts: []Part{
				Text{Text: "hello"},
				Text{Text: " "},
				Text{Text: "world"},
			},
			want: "hello world",
		},
		{
			name: "ignores non-text parts",
			parts: []Part{
				Text{Text: "before"},
				DataPart{Bytes: []byte("ignored"), MIME: "text/plain"},
				Text{Text: "after"},
			},
			want: "beforeafter",
		},
		{
			name:  "empty parts",
			parts: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := TextFromParts(tt.parts); got != tt.want {
				t.Fatalf("TextFromParts() = %q, want %q", got, tt.want)
			}
		})
	}
}
