package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

type fakeTool struct{}

func (fakeTool) Name() string        { return "get_weather" }
func (fakeTool) Description() string { return "Get weather" }
func (fakeTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"city": {Type: "string"},
		},
		Required: []string{"city"},
	}
}
func (fakeTool) OutputSchema() *jsonschema.Schema               { return nil }
func (fakeTool) Handle(context.Context, string) (string, error) { return "", nil }

func TestGenerate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/chat"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if got, want := r.Header.Get("X-Test"), "yes"; got != want {
			t.Fatalf("header X-Test = %q, want %q", got, want)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"model":"llama3.2"`) {
			t.Fatalf("request model missing: %s", b)
		}
		if !strings.Contains(string(b), `"tools"`) {
			t.Fatalf("request tools missing: %s", b)
		}
		if !strings.Contains(string(b), `"format"`) {
			t.Fatalf("request format missing: %s", b)
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":2}`))
	}))
	defer srv.Close()

	model := NewModel("llama3.2", Config{BaseURL: srv.URL, Headers: map[string]string{"X-Test": "yes"}})
	resp, err := model.Generate(context.Background(), &blades.ModelRequest{
		Instruction: blades.SystemMessage("be concise"),
		Messages:    []*blades.Message{blades.UserMessage("hello")},
		Tools:       []tools.Tool{fakeTool{}},
		OutputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"answer": {Type: "string"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got, want := resp.Message.Text(), "hi"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := resp.Message.TokenUsage.TotalTokens, int64(5); got != want {
		t.Fatalf("total tokens = %d, want %d", got, want)
	}
}

func TestNewStreaming_WithToolCalls(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"content\":\"hel\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"message\":{\"role\":\"assistant\",\"tool_calls\":[{\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":{\"city\":\"Paris\"}}}]},\"done\":true,\"done_reason\":\"stop\"}\n"))
	}))
	defer srv.Close()

	model := NewModel("llama3.2", Config{BaseURL: srv.URL})
	var chunks []*blades.ModelResponse
	for resp, err := range model.NewStreaming(context.Background(), &blades.ModelRequest{Messages: []*blades.Message{blades.UserMessage("hello")}}) {
		if err != nil {
			t.Fatalf("stream error = %v", err)
		}
		chunks = append(chunks, resp)
	}
	if got, want := len(chunks), 2; got != want {
		t.Fatalf("chunk count = %d, want %d", got, want)
	}
	if got, want := chunks[0].Message.Text(), "hel"; got != want {
		t.Fatalf("chunk text = %q, want %q", got, want)
	}
	if got, want := chunks[1].Message.Role, blades.RoleTool; got != want {
		t.Fatalf("role = %s, want %s", got, want)
	}
	toolPart, ok := chunks[1].Message.Parts[0].(blades.ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want blades.ToolPart", chunks[1].Message.Parts[0])
	}
	if got, want := toolPart.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
}

func TestToChatMessages_ImageAndToolMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "img.bin")
	if err := os.WriteFile(path, []byte("test-image"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msgs, err := toChatMessages(&blades.Message{
		Role: blades.RoleUser,
		Parts: []blades.Part{
			blades.TextPart{Text: "describe"},
			blades.FilePart{URI: path, MIMEType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("toChatMessages error = %v", err)
	}
	if got, want := msgs[0].Images[0], base64.StdEncoding.EncodeToString([]byte("test-image")); got != want {
		t.Fatalf("encoded image mismatch")
	}

	toolMsgs, err := toChatMessages(&blades.Message{
		Role: blades.RoleTool,
		Parts: []blades.Part{
			blades.ToolPart{ID: "call_1", Name: "get_weather", Response: `{"temp":21}`},
		},
	})
	if err != nil {
		t.Fatalf("toChatMessages(tool) error = %v", err)
	}
	if got, want := len(toolMsgs), 2; got != want {
		t.Fatalf("tool message count = %d, want %d", got, want)
	}
	if got, want := toolMsgs[0].Role, string(blades.RoleAssistant); got != want {
		t.Fatalf("assistant role = %q, want %q", got, want)
	}
	if got, want := toolMsgs[1].ToolCallID, "call_1"; got != want {
		t.Fatalf("tool call id = %q, want %q", got, want)
	}
}

func TestFromChatResponse_DefaultToolArguments(t *testing.T) {
	t.Parallel()

	resp := fromChatResponse(chatResponse{
		Done: true,
		Message: chatMessage{ToolCalls: []chatToolCall{{
			ID: "x",
			Function: chatFunctionCall{
				Name: "fn",
			},
		}}},
	})
	part := resp.Message.Parts[0].(blades.ToolPart)
	if got, want := part.Request, "{}"; got != want {
		t.Fatalf("request = %q, want %q", got, want)
	}

	withArgs := fromChatResponse(chatResponse{
		Done: true,
		Message: chatMessage{ToolCalls: []chatToolCall{{
			Function: chatFunctionCall{Name: "fn", Arguments: json.RawMessage(`{"a":1}`)},
		}}},
	})
	part = withArgs.Message.Parts[0].(blades.ToolPart)
	if got, want := part.Request, `{"a":1}`; got != want {
		t.Fatalf("request = %q, want %q", got, want)
	}
}

func TestEncodeImageFromURI_DataURI(t *testing.T) {
	t.Parallel()

	encoded, err := encodeImageFromURI("data:image/png;base64,dGVzdA==")
	if err != nil {
		t.Fatalf("encodeImageFromURI error = %v", err)
	}
	if got, want := encoded, "dGVzdA=="; got != want {
		t.Fatalf("encoded = %q, want %q", got, want)
	}
}

func TestRawJSON_InvalidInput(t *testing.T) {
	t.Parallel()

	got := rawJSON("city=Paris")
	if !json.Valid(got) {
		t.Fatalf("rawJSON output is not valid json: %s", got)
	}
	if string(got) != `"city=Paris"` {
		t.Fatalf("rawJSON quoted result = %s", got)
	}
}

func TestGenerateNilRequest(t *testing.T) {
	t.Parallel()

	model := NewModel("llama3.2", Config{})
	_, err := model.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestEncodeImageFromURI_DataURIPlainText(t *testing.T) {
	t.Parallel()

	encoded, err := encodeImageFromURI("data:text/plain,hello%20world")
	if err != nil {
		t.Fatalf("encodeImageFromURI error = %v", err)
	}
	if got, want := encoded, base64.StdEncoding.EncodeToString([]byte("hello world")); got != want {
		t.Fatalf("encoded = %q, want %q", got, want)
	}
}
