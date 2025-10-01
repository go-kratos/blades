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

// Client provides a unified interface for both Vertex AI and GenAI backends
type Client struct {
	genaiClient *genai.Client
}

// NewClient creates a new Gemini client with the given genai.ClientConfig
func NewClient(ctx context.Context, clientConfig *genai.ClientConfig) (*Client, error) {
	if clientConfig == nil {
		return nil, fmt.Errorf("clientConfig cannot be nil")
	}

	genaiClient, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("creating GenAI client: %w", err)
	}

	return &Client{
		genaiClient: genaiClient,
	}, nil
}

// Generate generates content using the configured backend
// Returns blades.ModelResponse instead of SDK-specific types
func (c *Client) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	if c.genaiClient == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Apply model options with default MaxIterations like OpenAI
	opt := blades.ModelOptions{MaxIterations: 3}
	for _, apply := range opts {
		apply(&opt)
	}

	return c.generateWithIterations(ctx, req, opt)
}

// generateWithIterations handles the recursive tool calling logic
func (c *Client) generateWithIterations(ctx context.Context, req *blades.ModelRequest, opt blades.ModelOptions) (*blades.ModelResponse, error) {
	// Ensure we have at least one iteration left
	if opt.MaxIterations < 1 {
		return nil, ErrTooManyIterations
	}

	// Convert Blades request to GenAI format
	contents, systemInstruction, err := ConvertBladesToGenAI(req)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}

	// Convert model options and Gemini config to generation config
	generateConfig := &genai.GenerateContentConfig{}
	if systemInstruction != nil {
		generateConfig.SystemInstruction = systemInstruction
	}

	if opt.Temperature > 0 {
		temp := float32(opt.Temperature)
		generateConfig.Temperature = &temp
	}

	if opt.MaxOutputTokens > 0 {
		generateConfig.MaxOutputTokens = int32(opt.MaxOutputTokens)
	}

	if opt.TopP > 0 {
		topP := float32(opt.TopP)
		generateConfig.TopP = &topP
	}

	// Apply Gemini-specific configuration from ModelOptions
	if opt.ThinkingBudget != nil || opt.IncludeThoughts != nil {
		// Configure thinking with budget and include thoughts options
		thinkingConfig := &genai.ThinkingConfig{}

		if opt.ThinkingBudget != nil {
			thinkingConfig.ThinkingBudget = opt.ThinkingBudget
		}

		if opt.IncludeThoughts != nil {
			thinkingConfig.IncludeThoughts = *opt.IncludeThoughts
		}

		generateConfig.ThinkingConfig = thinkingConfig
	}

	// Convert tools if provided
	if len(req.Tools) > 0 {
		genaiTools, err := ConvertBladesToolsToGenAI(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("converting tools: %w", err)
		}
		generateConfig.Tools = genaiTools
	}

	// Use the GenAI client for both backends since they use the same interface
	resp, err := c.genaiClient.Models.GenerateContent(ctx, req.Model, contents, generateConfig)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}

	// Convert response and handle tool execution inline
	response, err := ConvertGenAIToBlades(resp)
	if err != nil {
		return nil, err
	}

	// Handle tool calls if any - following OpenAI pattern
	for _, msg := range response.Messages {
		if len(msg.ToolCalls) > 0 {
			// Add the assistant's message with tool calls to conversation history
			assistantMsg := &blades.Message{
				Role:      blades.RoleAssistant,
				Parts:     msg.Parts,
				ToolCalls: msg.ToolCalls,
			}
			req.Messages = append(req.Messages, assistantMsg)

			// Execute each tool call and add results to conversation
			for _, tc := range msg.ToolCalls {
				// Execute the tool call
				result, err := toolCall(ctx, req.Tools, tc.Name, tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("executing tool %s: %w", tc.Name, err)
				}
				// Set the result
				tc.Result = result

				// Add tool result message to conversation history for the LLM
				toolResultMsg := &blades.Message{
					Role:      blades.RoleTool,
					ToolCalls: []*blades.ToolCall{tc},
				}
				req.Messages = append(req.Messages, toolResultMsg)
			}

			// Set message role to Tool for the response
			msg.Role = blades.RoleTool

			// Recursively call generateWithIterations to handle tool response continuation
			opt.MaxIterations--
			return c.generateWithIterations(ctx, req, opt)
		}
	}

	return response, nil
}

// GenerateStream generates streaming content using the configured backend
// Returns blades.Streamer[*blades.ModelResponse] following openai pattern
func (c *Client) GenerateStream(ctx context.Context, req *blades.ModelRequest, opt blades.ModelOptions) (blades.Streamer[*blades.ModelResponse], error) {
	// Ensure we have at least one iteration left
	if opt.MaxIterations < 1 {
		return nil, ErrTooManyIterations
	}

	// Convert Blades request to GenAI format
	contents, systemInstruction, err := ConvertBladesToGenAI(req)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}

	// Convert model options and Gemini config to generation config
	generateConfig := &genai.GenerateContentConfig{}
	if systemInstruction != nil {
		generateConfig.SystemInstruction = systemInstruction
	}

	if opt.Temperature > 0 {
		temp := float32(opt.Temperature)
		generateConfig.Temperature = &temp
	}

	if opt.MaxOutputTokens > 0 {
		generateConfig.MaxOutputTokens = int32(opt.MaxOutputTokens)
	}

	if opt.TopP > 0 {
		topP := float32(opt.TopP)
		generateConfig.TopP = &topP
	}

	// Apply Gemini-specific configuration from ModelOptions
	if opt.ThinkingBudget != nil || opt.IncludeThoughts != nil {
		// Configure thinking with budget and include thoughts options
		thinkingConfig := &genai.ThinkingConfig{}

		if opt.ThinkingBudget != nil {
			thinkingConfig.ThinkingBudget = opt.ThinkingBudget
		}

		if opt.IncludeThoughts != nil {
			thinkingConfig.IncludeThoughts = *opt.IncludeThoughts
		}

		generateConfig.ThinkingConfig = thinkingConfig
	}

	// Convert tools if provided
	if len(req.Tools) > 0 {
		genaiTools, err := ConvertBladesToolsToGenAI(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("converting tools: %w", err)
		}
		generateConfig.Tools = genaiTools
	}

	// Create stream pipe like in openai
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	pipe.Go(func() error {
		// Get streaming iterator from GenAI client
		stream := c.genaiClient.Models.GenerateContentStream(ctx, req.Model, contents, generateConfig)

		// Accumulate chunks to build final response for tool call handling
		var accumulatedResponse *genai.GenerateContentResponse

		// Process stream chunks using iterator pattern
		for chunk, err := range stream {
			if err != nil {
				return err
			}

			// Convert chunk to Blades response and send immediately
			response, err := ConvertStreamChunkToBlades(chunk)
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
			finalResponse, err := ConvertGenAIToBlades(accumulatedResponse)
			if err != nil {
				return err
			}

			// Handle tool calls if any - following OpenAI pattern
			for _, msg := range finalResponse.Messages {
				if len(msg.ToolCalls) > 0 {
					// Add the assistant's message with tool calls to conversation history
					assistantMsg := &blades.Message{
						Role:      blades.RoleAssistant,
						Parts:     msg.Parts,
						ToolCalls: msg.ToolCalls,
					}
					req.Messages = append(req.Messages, assistantMsg)

					// Execute each tool call and add results to conversation
					for _, tc := range msg.ToolCalls {
						// Execute the tool call
						result, err := toolCall(ctx, req.Tools, tc.Name, tc.Arguments)
						if err != nil {
							return err
						}
						// Set the result
						tc.Result = result

						// Add tool result message to conversation history for the LLM
						toolResultMsg := &blades.Message{
							Role: blades.RoleTool,
							Parts: []blades.Part{
								blades.TextPart{Text: result},
							},
							ToolCalls: []*blades.ToolCall{tc},
						}
						req.Messages = append(req.Messages, toolResultMsg)
					}

					// Recursively call GenerateStream to handle tool response continuation
					opt.MaxIterations--
					toolStream, err := c.GenerateStream(ctx, req, opt)
					if err != nil {
						return err
					}

					// Forward all responses from the tool stream
					for toolStream.Next() {
						toolResponse, err := toolStream.Current()
						if err != nil {
							return err
						}
						pipe.Send(toolResponse)
					}
				}
			}
		}

		return nil
	})

	return pipe, nil
}

// NewStream is an alias for GenerateStream to implement the ModelProvider interface
func (c *Client) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamer[*blades.ModelResponse], error) {
	if c.genaiClient == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	opt := blades.ModelOptions{MaxIterations: 3}
	for _, apply := range opts {
		apply(&opt)
	}
	if opt.MaxIterations > 0 {
		opt.MaxIterations--
	} else {
		return nil, ErrTooManyIterations
	}
	return c.GenerateStream(ctx, req, opt)
}

// toolCall invokes a tool by name with the given arguments.
func toolCall(ctx context.Context, tools []*blades.Tool, name, arguments string) (string, error) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Handle(ctx, arguments)
		}
	}
	return "", ErrToolNotFound
}
