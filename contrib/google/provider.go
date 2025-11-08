package google

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
	"google.golang.org/genai"
)

var _ blades.ModelProvider = (*Provider)(nil)

// Option defines a configuration option for the Provider.
type Option func(*Options)

// WithThinkingConfig sets the thinking config for the provider.
func WithThinkingConfig(c *genai.ThinkingConfig) Option {
	return func(o *Options) {
		o.ThinkingConfig = c
	}
}

// Options holds configuration options for the Provider.
type Options struct {
	ThinkingConfig *genai.ThinkingConfig
}

// Provider provides a unified interface for Gemini API access.
type Provider struct {
	opts   Options
	client *genai.Client
}

func NewProvider(ctx context.Context, clientConfig *genai.ClientConfig, opts ...Option) (*Provider, error) {
	opt := Options{}
	for _, apply := range opts {
		apply(&opt)
	}
	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, err
	}
	return &Provider{
		opts:   opt,
		client: client,
	}, nil
}

func (c *Provider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	system, contents, err := convertMessageToGenAI(req)
	if err != nil {
		return nil, err
	}
	config, err := c.toGenerateConfig(req, opt)
	if err != nil {
		return nil, err
	}
	config.SystemInstruction = system
	resp, err := c.client.Models.GenerateContent(ctx, req.Model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}
	return convertGenAIToBlades(resp)
}

func (c *Provider) toGenerateConfig(req *blades.ModelRequest, opt blades.ModelOptions) (*genai.GenerateContentConfig, error) {
	var config genai.GenerateContentConfig
	if opt.Temperature > 0 {
		temperature := float32(opt.Temperature)
		config.Temperature = &temperature
	}
	if opt.MaxOutputTokens > 0 {
		config.MaxOutputTokens = int32(opt.MaxOutputTokens)
	}
	if opt.TopP > 0 {
		topP := float32(opt.TopP)
		config.TopP = &topP
	}
	if c.opts.ThinkingConfig != nil {
		config.ThinkingConfig = c.opts.ThinkingConfig
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
func (c *Provider) NewStreaming(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) stream.Streamable[*blades.ModelResponse] {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	return func(yield func(*blades.ModelResponse, error) bool) {
		system, contents, err := convertMessageToGenAI(req)
		if err != nil {
			yield(nil, err)
			return
		}
		config, err := c.toGenerateConfig(req, opt)
		if err != nil {
			yield(nil, err)
			return
		}
		config.SystemInstruction = system
		streaming := c.client.Models.GenerateContentStream(ctx, req.Model, contents, config)
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
