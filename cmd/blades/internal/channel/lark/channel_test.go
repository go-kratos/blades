package lark

import (
	"testing"
	"time"
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
