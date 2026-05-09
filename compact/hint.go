package compact

import "context"

type hintKey struct{}

// HintShrink is the context hint value requesting aggressive compaction.
const HintShrink = "shrink"

// WithHint injects a compaction hint into the context.
func WithHint(ctx context.Context, hint string) context.Context {
	return context.WithValue(ctx, hintKey{}, hint)
}

// GetHint retrieves the compaction hint from the context.
func GetHint(ctx context.Context) string {
	if v, ok := ctx.Value(hintKey{}).(string); ok {
		return v
	}
	return ""
}
