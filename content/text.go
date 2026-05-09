package content

// Text is a plain text content part.
type Text struct {
	Text string
}

func (Text) part() {}
