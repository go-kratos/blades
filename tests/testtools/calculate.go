package testtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/tidwall/gjson"
)

var expressionPattern = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*([+\-*/])\s*(-?\d+(?:\.\d+)?)\s*$`)

type CalculateTool struct{}

func NewCalculateTool() CalculateTool {
	return CalculateTool{}
}

func Calculate(expression string) (string, error) {
	matches := expressionPattern.FindStringSubmatch(expression)
	if matches == nil {
		return "", fmt.Errorf("unsupported expression %q", expression)
	}

	left, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return "", err
	}
	right, err := strconv.ParseFloat(matches[3], 64)
	if err != nil {
		return "", err
	}

	var result float64
	switch matches[2] {
	case "+":
		result = left + right
	case "-":
		result = left - right
	case "*":
		result = left * right
	case "/":
		result = left / right
	default:
		return "", fmt.Errorf("unsupported operator %q", matches[2])
	}

	return fmt.Sprintf("%s = %s", expression, formatNumber(result)), nil
}

func (CalculateTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "calculate",
		Description: "Evaluate mathematical expressions",
		InputSchema: &jsonschema.Schema{Type: "object"},
	}
}

func (CalculateTool) Handle(_ context.Context, input json.RawMessage) (*tools.Result, error) {
	expression := gjson.GetBytes(input, "expression").String()
	if expression == "" {
		return nil, errors.New("expression is required")
	}

	result, err := Calculate(expression)
	if err != nil {
		return nil, err
	}
	return tools.TextResult(result), nil
}

func formatNumber(value float64) string {
	if math.Trunc(value) == value {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
