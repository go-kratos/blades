package openai

import (
	"encoding/json"

	"github.com/go-kratos/blades/model"
	openaisdk "github.com/openai/openai-go/v3"
)

func rawUsageJSON(raw string) json.RawMessage {
	return json.RawMessage(raw)
}

func completionUsageToModel(usage openaisdk.CompletionUsage) model.Usage {
	return model.Usage{
		InputCachedTokens:        usage.PromptTokensDetails.CachedTokens,
		InputCacheMissTokens:     usage.PromptTokens - usage.PromptTokensDetails.CachedTokens,
		InputAudioTokens:         usage.PromptTokensDetails.AudioTokens,
		OutputAudioTokens:        usage.CompletionTokensDetails.AudioTokens,
		OutputReasoningTokens:    usage.CompletionTokensDetails.ReasoningTokens,
		AcceptedPredictionTokens: usage.CompletionTokensDetails.AcceptedPredictionTokens,
		RejectedPredictionTokens: usage.CompletionTokensDetails.RejectedPredictionTokens,
		TotalInputTokens:         usage.PromptTokens,
		TotalOutputTokens:        usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		Raw:                      rawUsageJSON(usage.RawJSON()),
	}
}

func imageUsageToModel(usage openaisdk.ImagesResponseUsage) model.Usage {
	return model.Usage{
		InputCacheMissTokens: usage.InputTokens,
		InputTextTokens:      usage.InputTokensDetails.TextTokens,
		InputImageTokens:     usage.InputTokensDetails.ImageTokens,
		OutputImageTokens:    usage.OutputTokens,
		TotalInputTokens:     usage.InputTokens,
		TotalOutputTokens:    usage.OutputTokens,
		TotalTokens:          usage.TotalTokens,
		Raw:                  rawUsageJSON(usage.RawJSON()),
	}
}
