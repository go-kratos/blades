package google

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"google.golang.org/genai"
)

// Option defines a configuration option for the Provider.
type Option func(*options)

// WithTemperature sets the temperature for the model.
func WithTemperature(t float32) Option {
	return func(o *options) {
		o.Temperature = &t
	}
}

// WithTopP sets the top-p value for the model.
func WithTopP(p float32) Option {
	return func(o *options) {
		o.TopP = &p
	}
}

// WithTopK sets the top-k value for the model.
func WithTopK(k float32) Option {
	return func(o *options) {
		o.TopK = &k
	}
}

// WithCandidateCount sets the number of candidate responses to generate.
func WithCandidateCount(count int32) Option {
	return func(o *options) {
		o.CandidateCount = count
	}
}

// WithMaxOutputTokens sets the maximum number of output tokens.
func WithMaxOutputTokens(tokens int32) Option {
	return func(o *options) {
		o.MaxOutputTokens = tokens
	}
}

// WithStopSequences sets the stop sequences for the model.
func WithStopSequences(sequences []string) Option {
	return func(o *options) {
		o.StopSequences = sequences
	}
}

// WithResponseLogprobs enables or disables response logprobs.
func WithResponseLogprobs(enabled bool) Option {
	return func(o *options) {
		o.ResponseLogprobs = enabled
	}
}

// WithLogprobs sets the number of logprobs to return.
func WithLogprobs(logprobs int32) Option {
	return func(o *options) {
		o.Logprobs = &logprobs
	}
}

// WithPresencePenalty sets the presence penalty for the model.
func WithPresencePenalty(penalty float32) Option {
	return func(o *options) {
		o.PresencePenalty = &penalty
	}
}

// WithFrequencyPenalty sets the frequency penalty for the model.
func WithFrequencyPenalty(penalty float32) Option {
	return func(o *options) {
		o.FrequencyPenalty = &penalty
	}
}

// WithSeed sets the seed for the model.
func WithSeed(seed int32) Option {
	return func(o *options) {
		o.Seed = &seed
	}
}

// WithThinkingConfig sets the thinking config for the provider.
func WithThinkingConfig(c *genai.ThinkingConfig) Option {
	return func(o *options) {
		o.ThinkingConfig = c
	}
}

// options holds configuration options for the Provider.
type options struct {
	Temperature      *float32
	TopP             *float32
	TopK             *float32
	CandidateCount   int32
	MaxOutputTokens  int32
	StopSequences    []string
	ResponseLogprobs bool
	Logprobs         *int32
	PresencePenalty  *float32
	FrequencyPenalty *float32
	Seed             *int32
	ThinkingConfig   *genai.ThinkingConfig
}

// geminiModel provides a unified interface for Gemini API access.
type geminiModel struct {
	model  string
	opts   options
	client *genai.Client
}

// NewModel creates a new Gemini model provider.
func NewModel(ctx context.Context, model string, clientConfig *genai.ClientConfig, opts ...Option) (blades.ModelProvider, error) {
	modelOpts := options{}
	for _, apply := range opts {
		apply(&modelOpts)
	}
	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, err
	}
	return &geminiModel{
		model:  model,
		opts:   modelOpts,
		client: client,
	}, nil
}

// Name returns the name of the model.
func (m *geminiModel) Name() string {
	return m.model
}

func (m *geminiModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	system, contents, err := convertMessageToGenAI(req)
	if err != nil {
		return nil, err
	}
	config, err := m.toGenerateConfig(req)
	if err != nil {
		return nil, err
	}
	config.SystemInstruction = system
	resp, err := m.client.Models.GenerateContent(ctx, m.model, contents, config)
	if err != nil {
		return nil, err
	}
	return convertGenAIToBlades(resp)
}

func (m *geminiModel) toGenerateConfig(req *blades.ModelRequest) (*genai.GenerateContentConfig, error) {
	var config genai.GenerateContentConfig
	if m.opts.Temperature != nil {
		config.Temperature = m.opts.Temperature
	}
	if m.opts.TopP != nil {
		config.TopP = m.opts.TopP
	}
	if m.opts.TopK != nil {
		config.TopK = m.opts.TopK
	}
	if m.opts.CandidateCount > 0 {
		config.CandidateCount = m.opts.CandidateCount
	}
	if m.opts.MaxOutputTokens > 0 {
		config.MaxOutputTokens = m.opts.MaxOutputTokens
	}
	if len(m.opts.StopSequences) > 0 {
		config.StopSequences = m.opts.StopSequences
	}
	if m.opts.ResponseLogprobs {
		config.ResponseLogprobs = m.opts.ResponseLogprobs
	}
	if m.opts.Logprobs != nil {
		config.Logprobs = m.opts.Logprobs
	}
	if m.opts.PresencePenalty != nil {
		config.PresencePenalty = m.opts.PresencePenalty
	}
	if m.opts.FrequencyPenalty != nil {
		config.FrequencyPenalty = m.opts.FrequencyPenalty
	}
	if m.opts.Seed != nil {
		config.Seed = m.opts.Seed
	}
	if m.opts.ThinkingConfig != nil {
		config.ThinkingConfig = m.opts.ThinkingConfig
	}
	if len(req.Tools) > 0 {
		tools, err := convertBladesToolsToGenAI(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("converting tools: %w", err)
		}
		config.Tools = tools
	}
	return &config, nil
}

// NewStreaming is an alias for GenerateStream to implement the ModelProvider interface.
func (m *geminiModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		system, contents, err := convertMessageToGenAI(req)
		if err != nil {
			yield(nil, err)
			return
		}
		config, err := m.toGenerateConfig(req)
		if err != nil {
			yield(nil, err)
			return
		}
		config.SystemInstruction = system
		streaming := m.client.Models.GenerateContentStream(ctx, m.model, contents, config)
		var accumulatedResponse *genai.GenerateContentResponse
		for chunk, err := range streaming {
			if err != nil {
				yield(nil, err)
				return
			}
			response, err := convertGenAIToBlades(chunk)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(response, nil) {
				return
			}
			// Accumulate chunks
			if accumulatedResponse == nil {
				accumulatedResponse = chunk
			} else {
				if len(chunk.Candidates) > 0 && len(accumulatedResponse.Candidates) > 0 {
					candidate := accumulatedResponse.Candidates[0]
					chunkCandidate := chunk.Candidates[0]
					// Append parts from chunk to accumulated candidate
					if chunkCandidate.Content != nil {
						if candidate.Content == nil {
							candidate.Content = &genai.Content{Parts: []*genai.Part{}}
						}
						candidate.Content.Parts = append(candidate.Content.Parts, chunkCandidate.Content.Parts...)
					}
					// Update finish reason if present
					if chunkCandidate.FinishReason != "" {
						candidate.FinishReason = chunkCandidate.FinishReason
					}
				}
			}
		}
		// After streaming is complete, check for tool calls in accumulated response
		if accumulatedResponse != nil {
			finalResponse, err := convertGenAIToBlades(accumulatedResponse)
			if err != nil {
				yield(nil, err)
				return
			}
			finalResponse.Message.Status = blades.StatusCompleted
			yield(finalResponse, nil)
		}
	}
}
