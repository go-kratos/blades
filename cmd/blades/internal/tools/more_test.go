package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	bladestools "github.com/go-kratos/blades/tools"
)

type fixedIDSession struct {
	id string
}

func (s *fixedIDSession) ID() string { return s.id }

func (s *fixedIDSession) State() blades.State { return nil }

func (s *fixedIDSession) SetState(string, any) {}

func (s *fixedIDSession) History(context.Context) ([]*blades.Message, error) { return nil, nil }

func (s *fixedIDSession) Append(context.Context, *blades.Message) error { return nil }

func TestRegistryAndNormalizeHelpers(t *testing.T) {
	t.Parallel()

	reg := NewRegistry(map[string]bladestools.Tool{
		"cron": NewCronTool(cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)),
	})
	if _, err := reg.Resolve("cron"); err != nil {
		t.Fatalf("Resolve registered tool: %v", err)
	}
	if _, err := reg.Resolve("missing"); err == nil {
		t.Fatal("expected missing tool error")
	}

	if got := truncStr("abcdef", 4); got != "abc…" {
		t.Fatalf("truncStr = %q", got)
	}
	if got := normScheduleKind("delay"); got != cron.ScheduleAt {
		t.Fatalf("normScheduleKind(delay) = %q", got)
	}
	if got := normScheduleKind("EVERY"); got != cron.ScheduleEvery {
		t.Fatalf("normScheduleKind(EVERY) = %q", got)
	}
	if got := normPayloadKind("shell"); got != cron.PayloadExec {
		t.Fatalf("normPayloadKind(shell) = %q", got)
	}
	if got := normPayloadKind("message"); got != cron.PayloadAgentTurn {
		t.Fatalf("normPayloadKind(message) = %q", got)
	}
	if got := normPayloadKind("social"); got != cron.PayloadNotify {
		t.Fatalf("normPayloadKind(social) = %q", got)
	}
}

func TestCronToolAdditionalBranches(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), func(ctx context.Context, job *cron.Job) (string, error) {
		return "ran", nil
	})
	tool := &cronTool{svc: svc}

	out, err := tool.list()
	if err != nil || !strings.Contains(out, "No scheduled jobs") {
		t.Fatalf("empty list output = %q err=%v", out, err)
	}

	ctx := blades.NewSessionContext(context.Background(), &fixedIDSession{id: "chat-ctx"})

	added, err := tool.add(ctx, cronInput{
		Name:         "ctx-session",
		ScheduleType: "every",
		EverySeconds: 60,
		TaskType:     "agent",
		Prompt:       "ping",
	})
	if err != nil {
		t.Fatalf("add agent-turn job: %v", err)
	}
	if !strings.Contains(added, "Job added.") {
		t.Fatalf("add output = %q", added)
	}

	listed, err := tool.list()
	if err != nil {
		t.Fatalf("list non-empty: %v", err)
	}
	if !strings.Contains(listed, "ctx-session") || !strings.Contains(listed, "agent:ping") || !strings.Contains(listed, "chat-ctx") {
		t.Fatalf("list output = %q", listed)
	}

	if _, err := tool.add(context.Background(), cronInput{
		Name:         "missing-at",
		ScheduleType: "at",
		TaskType:     "agent",
		Prompt:       "x",
	}); err == nil {
		t.Fatal("expected missing at error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "missing-every",
		ScheduleType: "every",
		TaskType:     "agent",
		Prompt:       "x",
	}); err == nil {
		t.Fatal("expected missing every_seconds error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "bad-cron",
		ScheduleType: "cron",
		CronExpr:     "bad",
		TaskType:     "agent",
		Prompt:       "x",
	}); err == nil {
		t.Fatal("expected invalid cron expr error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "missing-command",
		ScheduleType: "every",
		EverySeconds: 1,
		TaskType:     "exec",
	}); err == nil {
		t.Fatal("expected missing command error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "missing-prompt",
		ScheduleType: "every",
		EverySeconds: 1,
		TaskType:     "agent",
	}); err == nil {
		t.Fatal("expected missing prompt error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "missing-chat-target",
		ScheduleType: "every",
		EverySeconds: 1,
		TaskType:     "notify",
		Text:         "x",
	}); err == nil {
		t.Fatal("expected missing chat target error")
	}
	if _, err := tool.add(context.Background(), cronInput{
		Name:         "bad-task",
		ScheduleType: "every",
		EverySeconds: 1,
		TaskType:     "weird",
		Prompt:       "x",
	}); err == nil {
		t.Fatal("expected unknown task type error")
	}

	jobs, err := svc.ListJobs(true)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %v err=%v", jobs, err)
	}

	if out, err := tool.remove(context.Background(), "missing"); err != nil || !strings.Contains(out, "not found") {
		t.Fatalf("remove missing output = %q err=%v", out, err)
	}
	if _, err := tool.remove(context.Background(), ""); err == nil {
		t.Fatal("expected remove missing id error")
	}
	if _, err := tool.run(context.Background(), ""); err == nil {
		t.Fatal("expected run missing id error")
	}

	raw, _ := json.Marshal(map[string]any{"action": "bogus"})
	if _, err := tool.handle(context.Background(), string(raw)); err == nil {
		t.Fatal("expected unknown action error")
	}
}
