package tools

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

func TestCronToolAddAcceptsLegacyShellPayloadKind(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	tool := &cronTool{svc: svc}

	result, err := tool.add(cronInput{
		Name:         "legacy-shell",
		PayloadKind:  "shell",
		Command:      "ls .",
		ScheduleKind: "at",
		AtMs:         time.Now().Add(time.Minute).UnixMilli(),
	})
	if err != nil {
		t.Fatalf("add legacy shell payload kind: %v", err)
	}
	if !strings.Contains(result, "Job added.") {
		t.Fatalf("unexpected add result %q", result)
	}

	jobs := svc.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if got := jobs[0].Payload.Kind; got != cron.PayloadExec {
		t.Fatalf("payload kind = %q, want %q", got, cron.PayloadExec)
	}
	if got := jobs[0].Payload.Command; got != "ls ." {
		t.Fatalf("payload command = %q, want %q", got, "ls .")
	}
}
