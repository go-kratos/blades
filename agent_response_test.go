package blades

import (
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/stretchr/testify/assert"
)

func TestThinkingOnlyAsText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		parts         []content.Part
		want          []content.Part
		wantConverted bool
	}{
		{
			name: "single thinking part",
			parts: []content.Part{
				content.Thinking{Text: "I will look that up.", Signature: []byte("signature")},
			},
			want: []content.Part{
				content.Text{Text: "I will look that up."},
			},
			wantConverted: true,
		},
		{
			name: "multiple thinking parts",
			parts: []content.Part{
				content.Thinking{Text: "first", Signature: []byte("one")},
				content.Thinking{Text: " second", Signature: []byte("two")},
			},
			want: []content.Part{
				content.Text{Text: "first second"},
			},
			wantConverted: true,
		},
		{
			name: "thinking with text is unchanged",
			parts: []content.Part{
				content.Thinking{Text: "reasoning"},
				content.Text{Text: "answer"},
			},
			want: []content.Part{
				content.Thinking{Text: "reasoning"},
				content.Text{Text: "answer"},
			},
		},
		{
			name: "thinking with tool use is unchanged",
			parts: []content.Part{
				content.Thinking{Text: "reasoning"},
				content.ToolUse{ID: "call-1", Name: "lookup"},
			},
			want: []content.Part{
				content.Thinking{Text: "reasoning"},
				content.ToolUse{ID: "call-1", Name: "lookup"},
			},
		},
		{
			name:  "empty parts are unchanged",
			parts: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, converted := thinkingOnlyAsText(tt.parts)
			assert.Equal(t, tt.wantConverted, converted)
			assert.Equal(t, tt.want, got)
		})
	}
}
