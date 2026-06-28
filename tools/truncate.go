package tools

import (
	"context"
	"fmt"
)

// Truncate returns a middleware that truncates tool results when they exceed
// a configurable character limit. When truncation occurs, a notice is appended
// indicating how many characters were omitted.
//
// This is useful for preventing large tool outputs (e.g. web scraping, code
// search results) from consuming excessive context window space in LLM calls.
//
// Example:
//
//	truncMw := tools.Truncate(4000)
//	myTool := tools.NewTool("search", "search the web", handler,
//	    tools.WithMiddleware(truncMw),
//	)
func Truncate(maxChars int) Middleware {
	return func(next Handler) Handler {
		return HandleFunc(func(ctx context.Context, input string) (string, error) {
			result, err := next.Handle(ctx, input)
			if err != nil {
				return result, err
			}
			if len(result) > maxChars {
				omitted := len(result) - maxChars
				return result[:maxChars] +
					fmt.Sprintf("\n\n[Output truncated: %d characters omitted]", omitted), nil
			}
			return result, nil
		})
	}
}
