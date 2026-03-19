package lark

import (
	"context"
	"strings"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestChannelName(t *testing.T) {
	ch := New()
	if ch.Name() != "lark" {
		t.Fatalf("name = %q, want %q", ch.Name(), "lark")
	}
}

func TestNewWithOptions(t *testing.T) {
	ch := New(
		WithAppID("test-app-id"),
		WithAppSecret("test-app-secret"),
		WithEncryptKey("test-encrypt-key"),
		WithVerificationToken("test-token"),
	)

	if ch.appID != "test-app-id" {
		t.Fatalf("appID = %q, want %q", ch.appID, "test-app-id")
	}
	if ch.appSecret != "test-app-secret" {
		t.Fatalf("appSecret = %q, want %q", ch.appSecret, "test-app-secret")
	}
	if ch.encryptKey != "test-encrypt-key" {
		t.Fatalf("encryptKey = %q, want %q", ch.encryptKey, "test-encrypt-key")
	}
	if ch.verificationToken != "test-token" {
		t.Fatalf("verificationToken = %q, want %q", ch.verificationToken, "test-token")
	}
}

func TestMarkMessageIfNew(t *testing.T) {
	ch := New()
	ch.dedupeTTL = time.Minute

	now := time.Unix(100, 0)
	if !ch.markMessageIfNew("msg-1", now) {
		t.Fatalf("first message should be accepted")
	}
	if ch.markMessageIfNew("msg-1", now.Add(30*time.Second)) {
		t.Fatalf("duplicate within TTL should be ignored")
	}
	if ch.markMessageIfNew("msg-1", now.Add(2*time.Minute)) {
		t.Fatalf("duplicate after TTL should still be ignored (no reprocess)")
	}
}

func TestSetSessionOverridesChatSession(t *testing.T) {
	ch := New()

	if got := ch.getOrCreateSession("chat-1", "p2p"); got != "chat-1" {
		t.Fatalf("default session = %q, want %q", got, "chat-1")
	}

	ch.setSession("chat-1", "custom-session")

	if got := ch.getOrCreateSession("chat-1", "p2p"); got != "custom-session" {
		t.Fatalf("overridden session = %q, want %q", got, "custom-session")
	}
}

func TestHandleMessageIgnoresNilEvent(t *testing.T) {
	ch := New()

	if err := ch.handleMessage(context.Background(), nil, nil); err != nil {
		t.Fatalf("handleMessage(nil): %v", err)
	}
	if err := ch.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{}, nil); err != nil {
		t.Fatalf("handleMessage(nil event data): %v", err)
	}
}

func TestHandleMessageRejectsNilHandler(t *testing.T) {
	ch := New()
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

	err := ch.handleMessage(context.Background(), event, nil)
	if err == nil || !strings.Contains(err.Error(), "stream handler is nil") {
		t.Fatalf("handleMessage nil handler error = %v", err)
	}
}
