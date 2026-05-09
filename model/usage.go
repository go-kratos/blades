package model

// Usage tracks token consumption for a model call.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopSafety    StopReason = "safety"
)
