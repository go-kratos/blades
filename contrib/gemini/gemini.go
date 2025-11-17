package gemini

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"google.golang.org/genai"
)

// Config holds configuration for the Gemini model.
type Config struct {
	APIKey           string
	Seed             int32
	MaxOutputTokens  int32
	Temperature      float32
	TopP             float32
	TopK             float32
	PresencePenalty  float32
	FrequencyPenalty float32
	StopSequences    []string
	ThinkingConfig   *genai.ThinkingConfig
}

// Gemini provides a unified interface for Gemini API access.
type Gemini struct {
	model  string
	config Config
	client *genai.Client
}

// NewModel creates a new Gemini model provider.
func NewModel(ctx context.Context, model string, config Config) (blades.ModelProvider, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: config.APIKey})
	if err != nil {
		return nil, err
	}
	return &Gemini{
		model:  model,
		config: config,
		client: client,
	}, nil
}

// Name returns the name of the model.
func (m *Gemini) Name() string {
	return m.model
}

func (m *Gemini) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
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

func (m *Gemini) toGenerateConfig(req *blades.ModelRequest) (*genai.GenerateContentConfig, error) {
	var config genai.GenerateContentConfig
	if m.config.Temperature > 0 {
		config.Temperature = &m.config.Temperature
	}
	if m.config.TopP > 0 {
		config.TopP = &m.config.TopP
	}
	if m.config.TopK > 0 {
		config.TopK = &m.config.TopK
	}
	if m.config.MaxOutputTokens > 0 {
		config.MaxOutputTokens = m.config.MaxOutputTokens
	}
	if len(m.config.StopSequences) > 0 {
		config.StopSequences = m.config.StopSequences
	}
	if m.config.PresencePenalty > 0 {
		config.PresencePenalty = &m.config.PresencePenalty
	}
	if m.config.FrequencyPenalty > 0 {
		config.FrequencyPenalty = &m.config.FrequencyPenalty
	}
	if m.config.Seed > 0 {
		config.Seed = &m.config.Seed
	}
	if m.config.ThinkingConfig != nil {
		config.ThinkingConfig = m.config.ThinkingConfig
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
func (m *Gemini) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
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
