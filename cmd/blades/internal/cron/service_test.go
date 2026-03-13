package cron
package cron

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewAgentHandlerAcceptsLegacyShellPayloadKind(t *testing.T) {
	t.Parallel()

	h := NewAgentHandler(nil, time.Second)
	output, err := h(context.Background(), &Job{
		Payload: Payload{
			Kind:    PayloadKind("shell"),
			Command: "printf ok",
		},
	})
	if err != nil {
		t.Fatalf("legacy shell payload execution: %v", err)
	}
	if strings.TrimSpace(output) != "ok" {
		t.Fatalf("output = %q, want %q", output, "ok")
	}
}