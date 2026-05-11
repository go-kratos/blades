package testtools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/tools"
	"github.com/stretchr/testify/assert"
)

func TestGetCurrentTimeWithTimezone(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 34, 56, 789000000, time.UTC)

	result, err := GetCurrentTime(now, "America/New_York")

	assert.NoError(t, err)
	assert.Equal(t, "Tuesday, May 12, 2026 at 8:34:56 AM EDT", result.Text)
	assert.Equal(t, now.UnixMilli(), result.UTCTimestamp)
}

func TestGetCurrentTimeRejectsInvalidTimezone(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 34, 56, 0, time.UTC)

	_, err := GetCurrentTime(now, "Invalid/Zone")

	assert.EqualError(t, err, "Invalid timezone: Invalid/Zone. Current UTC time: 2026-05-12T12:34:56Z")
}

func TestToolHandlesTimezoneInput(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 34, 56, 0, time.UTC)
	tool := NewCurrentTimeTool(WithCurrentTimeNow(func() time.Time { return now }))

	result, err := tool.Handle(context.Background(), json.RawMessage(`{"timezone":"Europe/London"}`))

	assert.NoError(t, err)
	assert.Equal(t, "Tuesday, May 12, 2026 at 1:34:56 PM BST", textFromResult(result))
}

func textFromResult(toolResult *tools.Result) string {
	var text string
	for _, part := range toolResult.Parts {
		textPart, ok := part.(content.Text)
		if ok {
			text += textPart.Text
		}
	}
	return text
}
