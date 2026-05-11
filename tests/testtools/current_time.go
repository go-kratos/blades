package testtools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/tidwall/gjson"
)

type Result struct {
	Text         string
	UTCTimestamp int64
}

type CurrentTimeTool struct {
	now func() time.Time
}

type CurrentTimeOption func(*CurrentTimeTool)

func NewCurrentTimeTool(opts ...CurrentTimeOption) CurrentTimeTool {
	tool := CurrentTimeTool{now: time.Now}
	for _, opt := range opts {
		opt(&tool)
	}
	return tool
}

func WithCurrentTimeNow(now func() time.Time) CurrentTimeOption {
	return func(tool *CurrentTimeTool) {
		tool.now = now
	}
}

func GetCurrentTime(now time.Time, timezone string) (Result, error) {
	if timezone != "" {
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return Result{}, fmt.Errorf("Invalid timezone: %s. Current UTC time: %s", timezone, now.UTC().Format(time.RFC3339Nano))
		}
		now = now.In(location)
	}

	return Result{
		Text:         now.Format("Monday, January 2, 2006 at 3:04:05 PM MST"),
		UTCTimestamp: now.UTC().UnixMilli(),
	}, nil
}

func (CurrentTimeTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{
		Name:        "get_current_time",
		Description: "Get the current date and time",
		InputSchema: &jsonschema.Schema{Type: "object"},
	}
}

func (tool CurrentTimeTool) Handle(_ context.Context, input json.RawMessage) (*tools.Result, error) {
	result, err := GetCurrentTime(tool.now(), gjson.GetBytes(input, "timezone").String())
	if err != nil {
		return nil, err
	}
	return tools.TextResult(result.Text), nil
}
