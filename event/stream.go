package event

// TextDelta is a hot-path streaming text fragment.
type TextDelta struct {
	Text string
}

func (TextDelta) output() {}

// ThinkingDelta is a hot-path streaming thinking/reasoning fragment.
type ThinkingDelta struct {
	Text      string
	Signature []byte
}

func (ThinkingDelta) output() {}
