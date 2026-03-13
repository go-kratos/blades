package counter

import "github.com/go-kratos/blades"

// charBasedCounter estimates tokens using character length divided by 4,
// which is a common heuristic for English text with OpenAI-compatible models.
type charBasedCounter struct{}

// NewCharBasedCounter returns a TokenCounter that approximates 1 token per 4
// characters. This is suitable as a default when no model-specific tokenizer
// is available.
func NewCharBasedCounter() blades.TokenCounter {
	return &charBasedCounter{}
}

// Count estimates token usage for the given messages.
func (c *charBasedCounter) Count(messages ...*blades.Message) int64 {
	var total int64
	for _, m := range messages {
		for _, p := range m.Parts {
			switch v := p.(type) {
			case blades.TextPart:
				total += int64(len(v.Text)+3) / 4
			case blades.ToolPart:
				total += int64(len(v.Name)+len(v.Request)+len(v.Response)+3) / 4
			}
		}
		// Approximate per-message overhead (role, metadata, etc.)
		total += 4
	}
	return total
}
