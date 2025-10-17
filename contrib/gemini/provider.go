package gemini

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kratos/blades"
	"google.golang.org/genai"
)

var (
	// ErrEmptyResponse indicates the provider returned no choices.
	ErrEmptyResponse = errors.New("empty completion response")
	// ErrToolNotFound indicates a tool call was made to an unknown tool.
	ErrToolNotFound = errors.New("tool not found")
	// ErrTooManyIterations indicates the max iterations option is less than 1.
	ErrTooManyIterations = errors.New("too many iterations requested")
)

// Option defines a configuration option for the Provider.
type Option func(*Options)

// WithThinkingConfig sets the thinking config for the provider.
func WithThinkingConfig(c *genai.ThinkingConfig) Option {
	return func(o *Options) {
		o.ThinkingConfig = c
	}
}

// WithMaxToolIterations sets the maximum number of tool iterations.
func WithMaxToolIterations(n int) Option {
	return func(o *Options) {
		o.MaxToolIterations = n
	}
}

// Options holds configuration options for the Provider.
type Options struct {
	MaxToolIterations int
	ThinkingConfig    *genai.ThinkingConfig
}

// Provider provides a unified interface for Gemini API access.
type Provider struct {
	opts   Options
	client *genai.Client
}

func NewProvider(ctx context.Context, clientConfig *genai.ClientConfig, opts ...Option) (*Provider, error) {
	opt := Options{MaxToolIterations: 5}
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
	return c.generate(ctx, req, opt, c.opts.MaxToolIterations)
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

func (c *Provider) generate(ctx context.Context, req *blades.ModelRequest, opt blades.ModelOptions, maxToolIterations int) (*blades.ModelResponse, error) {
	if maxToolIterations < 1 {
		return nil, ErrTooManyIterations
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
	// Use the GenAI client for both backends since they use the same interface
	resp, err := c.client.Models.GenerateContent(ctx, req.Model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}
	// Convert response and handle tool execution inline
	response, err := convertGenAIToBlades(resp)
	if err != nil {
		return nil, err
	}
	// TODO: handle tools
	return response, nil
}

// GenerateStream generates streaming content using the configured backend
// Returns blades.Streamer[*blades.ModelResponse] following openai pattern
func (c *Provider) GenerateStream(ctx context.Context, req *blades.ModelRequest, opt blades.ModelOptions, maxToolIterations int) (blades.Streamable[*blades.ModelResponse], error) {
	// Ensure we have at least one iteration left
	if maxToolIterations < 1 {
		return nil, ErrTooManyIterations
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
	// Create stream pipe like in openai
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	pipe.Go(func() error {
		// Get streaming iterator from GenAI client
		stream := c.client.Models.GenerateContentStream(ctx, req.Model, contents, config)
		// Accumulate chunks to build final response for tool call handling
		var accumulatedResponse *genai.GenerateContentResponse
		// Process stream chunks using iterator pattern
		for chunk, err := range stream {
			if err != nil {
				return err
			}
			// Convert chunk to Blades response and send immediately
			response, err := convertGenAIToBlades(chunk)
			if err != nil {
				return err
			}
			pipe.Send(response)
			// Accumulate chunks for final tool call processing
			if accumulatedResponse == nil {
				accumulatedResponse = chunk
			} else {
				// Merge chunk into accumulated response (simple approach)
				// In practice, you might need more sophisticated merging
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
				return err
			}
			finalResponse.Message.Status = blades.StatusCompleted
			// TODO: handle tools
			pipe.Send(finalResponse)
		}
		return nil
	})
	return pipe, nil
}

// NewStream is an alias for GenerateStream to implement the ModelProvider interface
func (c *Provider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamable[*blades.ModelResponse], error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	return c.GenerateStream(ctx, req, opt, c.opts.MaxToolIterations)
}
