package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
)

func TestLarkPureHelpers(t *testing.T) {
	ch := New(WithDebug(true), WithStop(func() error { return nil }), WithClearSession(func(string) error { return nil }))

	textContent := `{"text":"hello"}`
	msg := &larkim.EventMessage{Content: larkcore.StringPtr(textContent), MessageType: larkcore.StringPtr("text")}
	if got := ch.parseMessageContent(msg); got != "hello" {
		t.Fatalf("parseMessageContent(text) = %q", got)
	}

	postBody := map[string]any{
		"content": `[[
			{"tag":"text","text":"hello "},
			{"tag":"a","content":"world"}
		]]`,
	}
	postJSON, _ := json.Marshal(postBody)
	msg = &larkim.EventMessage{Content: larkcore.StringPtr(string(postJSON)), MessageType: larkcore.StringPtr("post")}
	if got := ch.parseMessageContent(msg); got != "hello world" {
		t.Fatalf("parseMessageContent(post) = %q", got)
	}

	interactiveJSON := `{"action":{"value":{"text":"clicked"}}}`
	msg = &larkim.EventMessage{Content: larkcore.StringPtr(interactiveJSON), MessageType: larkcore.StringPtr("interactive")}
	if got := ch.parseMessageContent(msg); got != "clicked" {
		t.Fatalf("parseMessageContent(interactive) = %q", got)
	}

	msg = &larkim.EventMessage{Content: larkcore.StringPtr("raw"), MessageType: larkcore.StringPtr("unknown")}
	if got := ch.parseMessageContent(msg); got != "raw" {
		t.Fatalf("parseMessageContent(fallback) = %q", got)
	}

	if got := ch.extractPostContent("not-json"); got != "not-json" {
		t.Fatalf("extractPostContent invalid = %q", got)
	}
	if got := ch.extractInteractiveContent("not-json"); got != "not-json" {
		t.Fatalf("extractInteractiveContent invalid = %q", got)
	}
	if got := truncForLog("abcdef", 3); got != "abc..." {
		t.Fatalf("truncForLog = %q", got)
	}
	if !isToolErrorOutput("Error: boom") || !isToolErrorOutput("oops\nExit: 1") || isToolErrorOutput("all good") {
		t.Fatal("unexpected isToolErrorOutput result")
	}

	if err := ch.sendTextMessage(context.Background(), "", "ignored"); err != nil {
		t.Fatalf("sendTextMessage empty chat: %v", err)
	}
	if err := ch.SendToSession(context.Background(), "", "ignored"); err != nil {
		t.Fatalf("SendToSession empty session: %v", err)
	}
}

func TestLarkCardBuilderHelpers(t *testing.T) {
	w := NewCardWriter(context.Background(), &lark.Client{}, "msg", "chat", "p2p")
	w.reactionSent = true
	w.textBuf.WriteString("hello")
	w.tools = []toolInfo{{
		id:     "1",
		name:   "exec",
		input:  `{"command":"pwd","working_dir":"/tmp"}`,
		output: "ok",
		status: "done",
	}}

	card := w.buildCard("hello")
	if !strings.Contains(card, "hello") || !strings.Contains(card, "\"tag\":\"markdown\"") {
		t.Fatalf("buildCard = %s", card)
	}
	finalCard := w.buildFinalCard("done")
	if !strings.Contains(finalCard, "done") || strings.Contains(finalCard, "▌") {
		t.Fatalf("buildFinalCard = %s", finalCard)
	}

	panel := buildToolPanel(toolInfo{name: "exec", input: `{"command":"pwd"}`, output: "ok", status: "done"})
	panelJSON, _ := json.Marshal(panel)
	if !strings.Contains(string(panelJSON), "exec") {
		t.Fatalf("buildToolPanel = %s", string(panelJSON))
	}

	if _, _, statusText, _ := toolPanelMeta("running"); statusText != "running" {
		t.Fatalf("toolPanelMeta(running) = %q", statusText)
	}
	if _, _, statusText, _ := toolPanelMeta("error"); statusText != "error" {
		t.Fatalf("toolPanelMeta(error) = %q", statusText)
	}

	block := buildToolCodeBlock(toolInfo{
		name:   "exec",
		input:  `{"command":"pwd","working_dir":"/tmp"}`,
		output: strings.Repeat("x", 20),
		status: "running",
	}, 5)
	if !strings.Contains(block, "# cwd: /tmp") || !strings.Contains(block, "...(truncated)") {
		t.Fatalf("buildToolCodeBlock = %s", block)
	}

	if cmd, wd := parseToolCommand(`{"command":"echo ok","working_dir":"/tmp"}`); cmd != "echo ok" || wd != "/tmp" {
		t.Fatalf("parseToolCommand = %q %q", cmd, wd)
	}
	if cmd, wd := parseToolCommand("bad json"); cmd != "" || wd != "" {
		t.Fatalf("parseToolCommand invalid = %q %q", cmd, wd)
	}
}

func TestLarkHTTPBackedPaths(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"token","expire":7200}`))
		case r.URL.Path == "/open-apis/im/v1/messages" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"card-1"}}`))
		case r.URL.Path == "/open-apis/im/v1/messages/card-1" && r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
		case r.URL.Path == "/open-apis/im/v1/messages/orig/reactions" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"reaction_id":"reaction-1"}}`))
		case r.URL.Path == "/open-apis/im/v1/messages/orig/reactions/reaction-1" && r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{}}`))
		}
	}))
	defer server.Close()

	client := lark.NewClient("app", "secret", lark.WithOpenBaseUrl(server.URL))
	ch := New(WithAppID("app"), WithAppSecret("secret"))
	ch.client = client

	if err := ch.sendTextMessage(context.Background(), "chat-1", "hello"); err != nil {
		t.Fatalf("sendTextMessage: %v", err)
	}
	if err := ch.sendErrorMessage(context.Background(), "chat-1", "boom"); err != nil {
		t.Fatalf("sendErrorMessage: %v", err)
	}
	if err := ch.SendToSession(context.Background(), "chat-1", "hello session"); err != nil {
		t.Fatalf("SendToSession: %v", err)
	}

	w := NewCardWriter(context.Background(), client, "orig", "chat-1", "p2p")
	w.flushInterval = 5 * time.Millisecond
	w.WriteText("hello")
	w.WriteEvent(channel.Event{Kind: channel.EventToolStart, ID: "1", Name: "exec", Input: `{"command":"pwd"}`})
	w.WriteEvent(channel.Event{Kind: channel.EventToolEnd, ID: "1", Name: "exec", Output: "ok"})
	time.Sleep(20 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	handlerCalled := false
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   larkcore.StringPtr("msg-1"),
				ChatId:      larkcore.StringPtr("chat-1"),
				ChatType:    larkcore.StringPtr("p2p"),
				MessageType: larkcore.StringPtr("text"),
				Content:     larkcore.StringPtr(`{"text":"hello"}`),
			},
		},
	}
	if err := ch.handleMessage(context.Background(), event, func(ctx context.Context, sid, text string, writer channel.Writer) (string, error) {
		handlerCalled = true
		writer.WriteText("reply")
		return "reply", nil
	}); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if !handlerCalled {
		t.Fatal("expected handleMessage to invoke handler")
	}

	cmdEvent := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   larkcore.StringPtr("msg-2"),
				ChatId:      larkcore.StringPtr("chat-1"),
				ChatType:    larkcore.StringPtr("p2p"),
				MessageType: larkcore.StringPtr("text"),
				Content:     larkcore.StringPtr(`{"text":"/help"}`),
			},
		},
	}
	if err := ch.handleMessage(context.Background(), cmdEvent, func(context.Context, string, string, channel.Writer) (string, error) {
		t.Fatal("handler should not be called for slash command")
		return "", nil
	}); err != nil {
		t.Fatalf("handleMessage command: %v", err)
	}

	if err := ch.handleCommand(context.Background(), "/session custom", "chat-1", "chat-1"); err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if got := ch.getOrCreateSession("chat-1", "p2p"); got != "custom" {
		t.Fatalf("session after handleCommand = %q", got)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	if len(gotRequests) == 0 {
		t.Fatal("expected local Lark HTTP requests")
	}
}
