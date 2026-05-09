package content

// Thinking represents model reasoning/thinking content.
// Signature carries provider verification data (e.g., Anthropic extended thinking).
type Thinking struct {
	Text      string
	Signature []byte
}

func (Thinking) part() {}
