package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

func TestEnsureHeartbeatJobSkipsExistingJob(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	schedule := cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: time.Minute.Milliseconds()}

	job1, existed, err := ensureHeartbeatJob(context.Background(), svc, schedule, defaultHeartbeatJobName, defaultHeartbeatMessage, defaultHeartbeatSessionID)
	if err != nil {
		t.Fatalf("first ensureHeartbeatJob: %v", err)
	}
	if existed {
		t.Fatal("first ensureHeartbeatJob reported existing job")
	}

	job2, existed, err := ensureHeartbeatJob(context.Background(), svc, schedule, defaultHeartbeatJobName, defaultHeartbeatMessage, defaultHeartbeatSessionID)
	if err != nil {
		t.Fatalf("second ensureHeartbeatJob: %v", err)
	}
	if !existed {
		t.Fatal("second ensureHeartbeatJob did not report existing job")
	}
	if job1.ID != job2.ID {
		t.Fatalf("expected existing job %q, got %q", job1.ID, job2.ID)
	}

	jobs := svc.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 heartbeat job, got %d", len(jobs))
	}
}

func TestRunNowReturnsOutput(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), func(ctx context.Context, job *cron.Job) (string, error) {
		return "listed files\n", nil
	})
	job, err := svc.AddJob(context.Background(), "test-output", cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: time.Minute.Milliseconds()}, cron.Payload{Kind: cron.PayloadExec, Command: "ls ."}, false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	output, err := svc.RunNow(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if output != "listed files\n" {
		t.Fatalf("unexpected output %q", output)
	}

	jobs := svc.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].State.LastOutput != output {
		t.Fatalf("expected last output %q, got %q", output, jobs[0].State.LastOutput)
	}
}
