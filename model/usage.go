package model

import (
	"encoding/json"
)

// Usage tracks normalized token consumption for a model call.
type Usage struct {
	// Input token breakdown.
	InputCachedTokens      int64 `json:"inputCachedTokens,omitempty"`
	InputCacheMissTokens   int64 `json:"inputCacheMissTokens,omitempty"`
	InputWriteCacheTokens  int64 `json:"inputWriteCacheTokens,omitempty"`
	InputCachedTextTokens  int64 `json:"inputCachedTextTokens,omitempty"`
	InputCachedImageTokens int64 `json:"inputCachedImageTokens,omitempty"`
	InputCachedAudioTokens int64 `json:"inputCachedAudioTokens,omitempty"`
	InputCachedVideoTokens int64 `json:"inputCachedVideoTokens,omitempty"`
	InputTextTokens        int64 `json:"inputTextTokens,omitempty"`
	InputImageTokens       int64 `json:"inputImageTokens,omitempty"`
	InputAudioTokens       int64 `json:"inputAudioTokens,omitempty"`
	InputVideoTokens       int64 `json:"inputVideoTokens,omitempty"`
	InputCitationTokens    int64 `json:"inputCitationTokens,omitempty"`
	InputToolTokens        int64 `json:"inputToolTokens,omitempty"`

	// Output token breakdown.
	OutputTextTokens      int64 `json:"outputTextTokens,omitempty"`
	OutputImageTokens     int64 `json:"outputImageTokens,omitempty"`
	OutputAudioTokens     int64 `json:"outputAudioTokens,omitempty"`
	OutputReasoningTokens int64 `json:"outputReasoningTokens,omitempty"`

	// Prediction token breakdown.
	AcceptedPredictionTokens int64 `json:"acceptedPredictionTokens,omitempty"`
	RejectedPredictionTokens int64 `json:"rejectedPredictionTokens,omitempty"`

	// Totals.
	TotalInputTokens  int64 `json:"totalInputTokens,omitempty"`
	TotalOutputTokens int64 `json:"totalOutputTokens,omitempty"`
	TotalTokens       int64 `json:"totalTokens,omitempty"`

	// Raw carries the provider-specific usage payload.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
	StopSafety    StopReason = "safety"
)
