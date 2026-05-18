package model

// ContextWindow describes the model's context capacity and compaction threshold.
type ContextWindow struct {
	MaxTokens    int64 // total model context window (e.g. 200_000)
	OutputTokens int64 // reserved for model output (e.g. 8_192)
}

// Threshold returns the token count at which compaction should trigger.
// Mirrors Claude Code: effectiveWindow = MaxTokens - min(OutputTokens, 20_000)
// Compact threshold = effectiveWindow - buffer (13_000)
func (w ContextWindow) Threshold() int64 {
	if w.MaxTokens <= 0 {
		return 0
	}
	reserved := min(w.OutputTokens, 20_000)
	effective := w.MaxTokens - reserved
	const buffer = 13_000
	if effective <= buffer {
		return 0
	}
	return effective - buffer
}
