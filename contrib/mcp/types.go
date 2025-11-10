package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toBladesTool converts an MCP tool to a Blades tool.
// This method is used by Provider to convert tools without creating separate Adapter instances.
func toBladesTool(mcpTool *mcp.Tool, handler tools.HandleFunc[string, string]) (tools.Tool, error) {
	// Convert the input schema
	inputSchema, err := convertSchema(mcpTool.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to convert input schema: %w", err)
	}
	// Convert the output schema if present
	var outputSchema *jsonschema.Schema
	if mcpTool.OutputSchema != nil {
		outputSchema, err = convertSchema(mcpTool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert output schema: %w", err)
		}
	}
	return tools.NewTool(
		mcpTool.Name,
		mcpTool.Description,
		handler,
		tools.WithInputSchema(inputSchema),
		tools.WithOutputSchema(outputSchema),
	), nil
}

// convertSchema converts an MCP schema to a Blades jsonschema.Schema.
func convertSchema(mcpSchema any) (*jsonschema.Schema, error) {
	if mcpSchema == nil {
		// If no schema provided, create an empty object schema
		return &jsonschema.Schema{
			Type: "object",
		}, nil
	}

	// Marshal the interface{} to JSON
	schemaBytes, err := json.Marshal(mcpSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Unmarshal into jsonschema.Schema
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	return &schema, nil
}

// formatToolResult converts MCP CallToolResult to a JSON string.
func formatToolResult(result *mcp.CallToolResult) (string, error) {
	// Check if the tool execution failed
	if result.IsError {
		// Extract error message from Content
		if len(result.Content) > 0 {
			// Try to extract text content as error message
			var errorMsg string
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					errorMsg += textContent.Text
				}
			}
			if errorMsg != "" {
				return "", fmt.Errorf("tool execution failed: %s", errorMsg)
			}
		}
		return "", fmt.Errorf("tool execution failed")
	}
	if result.StructuredContent != nil {
		outputBytes, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("failed to marshal structured content: %w", err)
		}
		return string(outputBytes), nil
	}
	outputBytes, err := json.Marshal(result.Content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}
	return string(outputBytes), nil
}
