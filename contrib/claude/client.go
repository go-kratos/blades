package claude

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

var (
	// ErrEmptyResponse indicates the provider returned no content.
	ErrEmptyResponse = errors.New("empty completion response")
	// ErrToolNotFound indicates a tool call was made to an unknown tool.
	ErrToolNotFound = errors.New("tool not found")
	// ErrTooManyIterations indicates the max iterations option is less than 1.
	ErrTooManyIterations = errors.New("too many iterations requested")
)

// Client provides a unified interface for Claude API access
type Client struct {
	client anthropic.Client
}

// NewClient creates a new Claude client with the given options
// Accepts official Anthropic SDK RequestOptions for maximum flexibility:
//   - Direct API: option.WithAPIKey("sk-...")
//   - AWS Bedrock: bedrock.WithLoadDefaultConfig(ctx)
//   - Google Vertex: vertex.WithGoogleAuth(ctx, region, projectID)
func NewClient(opts ...option.RequestOption) *Client {
	return &Client{
		client: anthropic.NewClient(opts...),
	}
}

// Generate generates content using the Claude API
// Returns blades.ModelResponse instead of SDK-specific types
func (c *Client) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	// Apply model options with default MaxIterations
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

	// Convert Blades request to Claude format
	params, err := ConvertBladesToClaude(req, opt)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}

	// Call standard API (supports extended thinking)
	message, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}

	// Convert response
	response, err := ConvertClaudeToBlades(message)
	if err != nil {
		return nil, err
	}

	// Handle tool calls if any - following pattern from gemini/openai
	msg := response.Message
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

	return response, nil
}

// NewStream executes the request and returns a stream of assistant responses
func (c *Client) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamable[*blades.ModelResponse], error) {
	// Apply model options with default MaxIterations
	opt := blades.ModelOptions{MaxIterations: 3}
	for _, apply := range opts {
		apply(&opt)
	}

	return c.generateStream(ctx, req, opt)
}

// generateStream generates streaming content using the Claude API
func (c *Client) generateStream(ctx context.Context, req *blades.ModelRequest, opt blades.ModelOptions) (blades.Streamable[*blades.ModelResponse], error) {
	// Ensure we have at least one iteration left
	if opt.MaxIterations < 1 {
		return nil, ErrTooManyIterations
	}

	// Convert Blades request to Claude format
	params, err := ConvertBladesToClaude(req, opt)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}

	// Create stream pipe like in openai/gemini
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	pipe.Go(func() error {
		// Get streaming from Claude API - returns iterator
		stream := c.client.Messages.NewStreaming(ctx, params)

		// Accumulate chunks to build final response for tool call handling
		message := &anthropic.Message{}

		// Process stream events using iterator pattern
		for stream.Next() {
			event := stream.Current()

			// Accumulate into final message for tool call handling
			if err := message.Accumulate(event); err != nil {
				return err
			}

			// Stream text deltas immediately to user
			if ev, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				response, err := ConvertStreamDeltaToBlades(ev)
				if err != nil {
					return err
				}
				if response != nil {
					pipe.Send(response)
				}
			}
		}

		if err := stream.Err(); err != nil {
			return err
		}

		// After streaming is complete, check for tool calls in accumulated message
		finalResponse, err := ConvertClaudeToBlades(message)
		if err != nil {
			return err
		}

		// Handle tool calls if any
		msg := finalResponse.Message
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

			// Recursively call generateStream to handle tool response continuation
			opt.MaxIterations--
			toolStream, err := c.generateStream(ctx, req, opt)
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

		return nil
	})

	return pipe, nil
}

// toolCall invokes a tool by name with the given arguments.
func toolCall(ctx context.Context, tools []*blades.Tool, name, arguments string) (string, error) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Handler.Handle(ctx, arguments)
		}
	}
	return "", ErrToolNotFound
}
