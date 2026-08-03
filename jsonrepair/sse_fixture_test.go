package jsonrepair

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRepairSSEFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename     string
		toolName     string
		stopped      bool
		expectedJSON string
	}{
		{
			filename:     "truncated-string-at-eof.sse",
			toolName:     "bash",
			stopped:      false,
			expectedJSON: `{"command":"printf 'fixture，content'"}`,
		},
		{
			filename:     "missing-object-close.sse",
			toolName:     "ask_user_question",
			stopped:      true,
			expectedJSON: `{"question":{"mode":"form","prompt":"Choose one："}}`,
		},
		{
			filename:     "unescaped-quotes.sse",
			toolName:     "ask_user_question",
			stopped:      true,
			expectedJSON: `{"question":{"prompt":"输入\"已登录\"后继续，或取消。"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			t.Parallel()

			fixture := readSSEFixture(t, filepath.Join("testdata", test.filename))
			if fixture.toolName != test.toolName {
				t.Fatalf("tool name = %q, want %q", fixture.toolName, test.toolName)
			}
			if fixture.stopped != test.stopped {
				t.Fatalf("content block stopped = %v, want %v", fixture.stopped, test.stopped)
			}
			if json.Valid(fixture.input) {
				t.Fatal("fixture tool input is valid JSON, want malformed reproduction")
			}
			var decoded any
			decodeErr := json.Unmarshal(fixture.input, &decoded)
			var syntaxError *json.SyntaxError
			if !errors.As(decodeErr, &syntaxError) {
				t.Fatalf("json.Unmarshal() error = %v, want *json.SyntaxError", decodeErr)
			}

			result, err := Repair(fixture.input)
			if err != nil {
				t.Fatalf("Repair() error = %v", err)
			}
			if got := string(result.JSON); got != test.expectedJSON {
				t.Fatalf("Repair() JSON = %q, want %q", got, test.expectedJSON)
			}
			if len(result.Edits) != 1 || result.Edits[0].Kind != EditRewriteDocument {
				t.Fatalf("edits = %#v, want one EditRewriteDocument", result.Edits)
			}
			assertRepairInvariants(t, fixture.input, result)
		})
	}
}

type toolInputFixture struct {
	toolName string
	input    []byte
	stopped  bool
}

type sseEvent struct {
	name string
	data string
}

func readSSEFixture(t *testing.T, filename string) toolInputFixture {
	t.Helper()

	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read SSE fixture: %v", err)
	}
	events := parseSSE(t, string(contents))
	if len(events) < 2 || len(events) > 3 {
		t.Fatalf("SSE fixture events = %d, want 2 or 3 tool events", len(events))
	}

	type eventData struct {
		Type         string `json:"type"`
		Index        int64  `json:"index"`
		ContentBlock struct {
			ID    string          `json:"id"`
			Input json.RawMessage `json:"input"`
			Name  string          `json:"name"`
			Type  string          `json:"type"`
		} `json:"content_block"`
		Delta struct {
			PartialJSON string `json:"partial_json"`
			Type        string `json:"type"`
		} `json:"delta"`
	}

	var fixture toolInputFixture
	started := false
	var toolIndex int64
	for position, event := range events {
		var current eventData
		if err := json.Unmarshal([]byte(event.data), &current); err != nil {
			t.Fatalf("event %d data is not valid JSON: %v", position, err)
		}
		if event.name != current.Type {
			t.Fatalf("event %d name = %q, data type = %q", position, event.name, current.Type)
		}

		switch current.Type {
		case "content_block_start":
			if started || position != 0 {
				t.Fatalf("event %d has duplicate or misplaced tool start", position)
			}
			assertJSONKeys(t, event.data, "content_block", "index", "type")
			assertNestedJSONKeys(t, event.data, "content_block", "id", "input", "name", "type")
			if current.ContentBlock.Type != "tool_use" || string(current.ContentBlock.Input) != "{}" {
				t.Fatalf("event %d is not an empty-input tool_use start", position)
			}
			if !strings.HasPrefix(current.ContentBlock.ID, "toolu_fixture_") {
				t.Fatalf("event %d tool ID %q is not synthetic", position, current.ContentBlock.ID)
			}
			started = true
			toolIndex = current.Index
			fixture.toolName = current.ContentBlock.Name
		case "content_block_delta":
			if !started || fixture.stopped || current.Index != toolIndex {
				t.Fatalf("event %d has invalid tool delta ordering or index", position)
			}
			assertJSONKeys(t, event.data, "delta", "index", "type")
			assertNestedJSONKeys(t, event.data, "delta", "partial_json", "type")
			if current.Delta.Type != "input_json_delta" {
				t.Fatalf("event %d delta type = %q, want input_json_delta", position, current.Delta.Type)
			}
			fixture.input = append(fixture.input, current.Delta.PartialJSON...)
		case "content_block_stop":
			if !started || fixture.stopped || current.Index != toolIndex || position != len(events)-1 {
				t.Fatalf("event %d has invalid tool stop ordering or index", position)
			}
			assertJSONKeys(t, event.data, "index", "type")
			fixture.stopped = true
		default:
			t.Fatalf("event %d type = %q, want only tool-call events", position, current.Type)
		}
	}
	if !started || len(fixture.input) == 0 {
		t.Fatal("SSE fixture does not contain tool input JSON deltas")
	}
	return fixture
}

func parseSSE(t *testing.T, input string) []sseEvent {
	t.Helper()

	var events []sseEvent
	var name string
	var data []string
	flush := func() {
		if name == "" && len(data) == 0 {
			return
		}
		if name == "" || len(data) == 0 {
			t.Fatalf("incomplete SSE event: event=%q, data lines=%d", name, len(data))
		}
		events = append(events, sseEvent{name: name, data: strings.Join(data, "\n")})
		name = ""
		data = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("invalid SSE field line %q", line)
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			if name != "" {
				t.Fatal("SSE event contains more than one event field")
			}
			name = value
		case "data":
			data = append(data, value)
		default:
			t.Fatalf("unexpected SSE field %q", field)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE fixture: %v", err)
	}
	flush()
	return events
}

func assertJSONKeys(t *testing.T, input string, expected ...string) {
	t.Helper()

	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatalf("decode JSON object for key validation: %v", err)
	}
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	slices.Sort(actual)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("JSON keys = %v, want %v", actual, expected)
	}
}

func assertNestedJSONKeys(t *testing.T, input, field string, expected ...string) {
	t.Helper()

	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatalf("decode JSON object for nested key validation: %v", err)
	}
	assertJSONKeys(t, string(value[field]), expected...)
}
