package lark

import (
	"strings"
	"testing"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

func TestNewFromConfigUsesEnvFallbacks(t *testing.T) {
	_, err := newFromConfig(config.LarkConfig{}, func(key string) string { return "" }, nil)
	if err == nil || !strings.Contains(err.Error(), "LARK_APP_ID") {
		t.Fatalf("expected missing app id error, got %v", err)
	}

	ch, err := newFromConfig(config.LarkConfig{
		EncryptKey:        "enc",
		VerificationToken: "verify",
		Debug:             true,
	}, func(key string) string {
		switch key {
		case "LARK_APP_ID":
			return "env-app-id"
		case "LARK_APP_SECRET":
			return "env-app-secret"
		default:
			return ""
		}
	}, nil)
	if err != nil {
		t.Fatalf("newFromConfig: %v", err)
	}
	if got, want := ch.Name(), "lark"; got != want {
		t.Fatalf("channel name = %q, want %q", got, want)
	}
}
