package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCronStore(t *testing.T, path string, st store) {
	t.Helper()

	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal store: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
}

func TestComputeNextRunAndHelpers(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC).UnixMilli()

	if got := computeNextRun(Schedule{Kind: ScheduleKind("once"), AtMs: base + 1_000}, base); got != base+1_000 {
		t.Fatalf("computeNextRun once = %d", got)
	}
	if got := computeNextRun(Schedule{Kind: ScheduleAt, AtMs: base - 1}, base); got != 0 {
		t.Fatalf("computeNextRun past at = %d", got)
	}
	if got := computeNextRun(Schedule{Kind: ScheduleEvery, EveryMs: 2_500}, base); got != base+2_500 {
		t.Fatalf("computeNextRun every = %d", got)
	}
	if got := computeNextRun(Schedule{Kind: ScheduleCron, Expr: "*/5 * * * *", TZ: "UTC"}, base); got <= base {
		t.Fatalf("computeNextRun cron = %d", got)
	}
	if got := computeNextRun(Schedule{Kind: ScheduleCron, Expr: "*/5 * * * *", TZ: "Bad/Zone"}, base); got <= base {
		t.Fatalf("computeNextRun invalid tz = %d", got)
	}
	if got := computeNextRun(Schedule{Kind: ScheduleCron, Expr: "not-a-cron", TZ: "UTC"}, base); got != 0 {
		t.Fatalf("computeNextRun invalid expr = %d", got)
	}

	jobs := []*Job{{ID: "a"}, {ID: "b"}}
	gotJobs := removeJobSlice(jobs, "a")
	if len(gotJobs) != 1 || gotJobs[0].ID != "b" {
		t.Fatalf("removeJobSlice = %+v", gotJobs)
	}
	if got := msToTime(0); got != "never" {
		t.Fatalf("msToTime(0) = %q", got)
	}
	if got := msToTime(base); !strings.Contains(got, "2026-03-18") {
		t.Fatalf("msToTime(base) = %q", got)
	}
	if ids := jobIDSet(gotJobs); len(ids) != 1 {
		t.Fatalf("jobIDSet = %+v", ids)
	}
}

func TestBotAndExecHandlers(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	var notifyCalls []string
	trigger := func(ctx context.Context, sessionID, text string) (string, error) {
		return sessionID + ":" + text, nil
	}
	notify := func(ctx context.Context, sessionID, text string) error {
		notifyCalls = append(notifyCalls, sessionID+":"+strings.TrimSpace(text))
		return nil
	}

	h := NewBotHandlerWithExecWorkDir(trigger, notify, time.Second, workDir)

	output, err := h(context.Background(), &Job{
		Name: "pwd",
		Payload: Payload{
			Kind:           PayloadExec,
			Command:        "pwd",
			ReplySessionID: "reply-exec",
		},
	})
	if err != nil {
		t.Fatalf("exec handler: %v", err)
	}
	if got := filepath.Clean(strings.TrimSpace(output)); got != filepath.Clean(workDir) {
		t.Fatalf("exec output = %q, want %q", got, workDir)
	}

	output, err = h(context.Background(), &Job{
		Name: "agent",
		Payload: Payload{
			Kind:           PayloadAgentTurn,
			Message:        "hello",
			ReplySessionID: "reply-agent",
		},
	})
	if err != nil {
		t.Fatalf("agent_turn handler: %v", err)
	}
	if output != "cron:hello" {
		t.Fatalf("agent_turn output = %q", output)
	}
	output, err = h(context.Background(), &Job{
		Name: "notify",
		Payload: Payload{
			Kind:           PayloadNotify,
			Message:        "hello channel",
			ReplySessionID: "reply-channel",
		},
	})
	if err != nil {
		t.Fatalf("notify handler: %v", err)
	}
	if output != "hello channel" {
		t.Fatalf("notify output = %q", output)
	}
	if len(notifyCalls) != 3 || notifyCalls[0] != "reply-exec:"+workDir || notifyCalls[1] != "reply-agent:cron:hello" || notifyCalls[2] != "reply-channel:hello channel" {
		t.Fatalf("notify calls = %+v", notifyCalls)
	}

	if _, err := h(context.Background(), &Job{
		Payload: Payload{
			Kind: PayloadKind("unknown"),
		},
	}); err == nil {
		t.Fatal("expected unknown payload error")
	}

	execOnly := DefaultExecHandler(0, workDir)
	if output, err := execOnly(context.Background(), &Job{
		Payload: Payload{
			Kind:    PayloadExec,
			Command: "pwd",
		},
	}); err != nil || filepath.Clean(strings.TrimSpace(output)) != filepath.Clean(workDir) {
		t.Fatalf("DefaultExecHandler exec output=%q err=%v", output, err)
	}
	if output, err := execOnly(context.Background(), &Job{
		Payload: Payload{
			Kind: PayloadAgentTurn,
		},
	}); err != nil || output != "" {
		t.Fatalf("DefaultExecHandler non-exec output=%q err=%v", output, err)
	}
}

func TestServiceManagementAndRunNow(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "cron.json")
	svc := NewService(storePath, nil)
	svc.SetHandler(func(ctx context.Context, job *Job) (string, error) {
		return "ran:" + job.Name, nil
	})

	ctx := context.Background()
	keepJob, err := svc.AddJob(ctx, "keep", Schedule{Kind: ScheduleEvery, EveryMs: time.Second.Milliseconds()}, Payload{Kind: PayloadExec, Command: "echo keep"}, false)
	if err != nil {
		t.Fatalf("AddJob keep: %v", err)
	}
	deleteJob, err := svc.AddJob(ctx, "delete", Schedule{Kind: ScheduleEvery, EveryMs: time.Second.Milliseconds()}, Payload{Kind: PayloadExec, Command: "echo delete"}, true)
	if err != nil {
		t.Fatalf("AddJob delete: %v", err)
	}

	jobs, err := svc.ListJobs(true)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("ListJobs includeDisabled jobs=%d err=%v", len(jobs), err)
	}

	ok, err := svc.EnableJob(ctx, keepJob.ID, false)
	if err != nil || !ok {
		t.Fatalf("EnableJob false ok=%v err=%v", ok, err)
	}
	jobs, err = svc.ListJobs(false)
	if err != nil || len(jobs) != 1 || jobs[0].ID != deleteJob.ID {
		t.Fatalf("ListJobs enabled-only = %+v err=%v", jobs, err)
	}

	ok, err = svc.EnableJob(ctx, keepJob.ID, true)
	if err != nil || !ok {
		t.Fatalf("EnableJob true ok=%v err=%v", ok, err)
	}
	jobs, err = svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs after enable: %v", err)
	}
	for _, job := range jobs {
		if job.ID == keepJob.ID && job.State.NextRunAtMs == 0 {
			t.Fatalf("re-enabled job missing next run: %+v", job)
		}
	}

	output, err := svc.RunNow(ctx, deleteJob.ID)
	if err != nil || output != "ran:delete" {
		t.Fatalf("RunNow output=%q err=%v", output, err)
	}
	jobs, err = svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs after RunNow: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != keepJob.ID {
		t.Fatalf("jobs after delete-after-run = %+v", jobs)
	}

	if _, err := svc.RunNow(ctx, "missing"); err == nil {
		t.Fatal("expected RunNow on missing job to fail")
	}
	if ok, err := svc.EnableJob(ctx, "missing", true); err != nil || ok {
		t.Fatalf("EnableJob missing ok=%v err=%v", ok, err)
	}
	if ok, err := svc.RemoveJob(ctx, keepJob.ID); err != nil || !ok {
		t.Fatalf("RemoveJob existing ok=%v err=%v", ok, err)
	}
	if ok, err := svc.RemoveJob(ctx, "missing"); err != nil || ok {
		t.Fatalf("RemoveJob missing ok=%v err=%v", ok, err)
	}
}

func TestStaleJobsAndFormatJob(t *testing.T) {
	t.Parallel()

	storePath := filepath.Join(t.TempDir(), "cron.json")
	now := time.Now()
	writeCronStore(t, storePath, store{
		Version: 1,
		Jobs: []*Job{
			{
				ID:      "stale",
				Name:    "stale-job",
				Enabled: true,
				Schedule: Schedule{
					Kind:    ScheduleEvery,
					EveryMs: time.Minute.Milliseconds(),
				},
				State: JobState{
					LastRunAtMs: now.Add(-2 * time.Hour).UnixMilli(),
				},
			},
			{
				ID:      "fresh",
				Name:    "fresh-job",
				Enabled: true,
				Schedule: Schedule{
					Kind: ScheduleCron,
					Expr: "0 9 * * *",
				},
				State: JobState{
					LastRunAtMs: now.Add(-10 * time.Minute).UnixMilli(),
					LastStatus:  "ok",
				},
			},
			{
				ID:      "disabled",
				Name:    "disabled-job",
				Enabled: false,
				Schedule: Schedule{
					Kind: ScheduleAt,
					AtMs: now.Add(time.Hour).UnixMilli(),
				},
				State: JobState{
					LastRunAtMs: now.Add(-3 * time.Hour).UnixMilli(),
				},
			},
		},
	})

	svc := NewService(storePath, nil)
	stale := svc.StaleJobs(time.Hour)
	if len(stale) != 1 || stale[0].ID != "stale" {
		t.Fatalf("StaleJobs = %+v", stale)
	}

	if got := FormatJob(&Job{
		ID:   "every",
		Name: "job-every",
		Schedule: Schedule{
			Kind:    ScheduleEvery,
			EveryMs: time.Second.Milliseconds(),
		},
	}); !strings.Contains(got, "every 1s") || !strings.Contains(got, "pending") {
		t.Fatalf("FormatJob every = %q", got)
	}
	if got := FormatJob(&Job{
		ID:   "cron",
		Name: "job-cron",
		Schedule: Schedule{
			Kind: ScheduleCron,
			Expr: "0 9 * * *",
		},
		State: JobState{LastStatus: "ok"},
	}); !strings.Contains(got, "cron(0 9 * * *)") || !strings.Contains(got, "ok") {
		t.Fatalf("FormatJob cron = %q", got)
	}
	if got := FormatJob(&Job{
		ID:   "notify",
		Name: "job-notify",
		Schedule: Schedule{
			Kind:    ScheduleEvery,
			EveryMs: time.Minute.Milliseconds(),
		},
		Payload: Payload{
			Kind:           PayloadNotify,
			Message:        "ping",
			ReplySessionID: "chat-123",
		},
	}); !strings.Contains(got, "notify -> chat:chat-123") {
		t.Fatalf("FormatJob notify = %q", got)
	}
}
