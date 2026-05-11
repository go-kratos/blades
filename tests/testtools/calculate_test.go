package testtools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/tools"
	"github.com/stretchr/testify/assert"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		want       string
	}{
		{name: "addition", expression: "12 + 30", want: "12 + 30 = 42"},
		{name: "subtraction", expression: "50 - 8", want: "50 - 8 = 42"},
		{name: "multiplication", expression: "123 * 456", want: "123 * 456 = 56088"},
		{name: "division", expression: "84 / 2", want: "84 / 2 = 42"},
		{name: "decimal result", expression: "5 / 2", want: "5 / 2 = 2.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Calculate(tt.expression)

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateRejectsUnsupportedExpression(t *testing.T) {
	_, err := Calculate("Math.max(1, 2)")

	assert.EqualError(t, err, `unsupported expression "Math.max(1, 2)"`)
}

func TestCalculateToolHandlesExpressionInput(t *testing.T) {
	tool := NewCalculateTool()

	result, err := tool.Handle(context.Background(), json.RawMessage(`{"expression":"123 * 456"}`))

	assert.NoError(t, err)
	assert.Equal(t, "123 * 456 = 56088", resultText(result))
}

func TestCalculateToolRequiresExpression(t *testing.T) {
	tool := NewCalculateTool()

	_, err := tool.Handle(context.Background(), json.RawMessage(`{}`))

	assert.EqualError(t, err, "expression is required")
}

func resultText(result *tools.Result) string {
	var text string
	for _, part := range result.Parts {
		textPart, ok := part.(content.Text)
		if ok {
			text += textPart.Text
		}
	}
	return text
}
