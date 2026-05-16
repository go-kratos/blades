package content

import "strings"

// Text is a plain text content part.
type Text struct {
	Text string
}

func (Text) part() {}

// TextFromParts concatenates text parts in order and ignores non-text parts.
func TextFromParts(parts []Part) string {
	var text strings.Builder
	for _, part := range parts {
		if p, ok := part.(Text); ok {
			text.WriteString(p.Text)
		}
	}
	return text.String()
}
