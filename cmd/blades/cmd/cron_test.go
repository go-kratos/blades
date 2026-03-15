package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

func TestParseDelayValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr string
	}{
		{name: "seconds without unit", raw: "10", want: 10 * time.Second},
		{name: "duration with unit", raw: "1m", want: time.Minute},
		{name: "fractional seconds", raw: "0.5", want: 500 * time.Millisecond},
		{name: "invalid value", raw: "abc", wantErr: "invalid --delay value"},
		{name: "non-positive value", raw: "0", wantErr: "--delay must be > 0"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDelayValue(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseDelayValue(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("parseDelayValue(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

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

	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
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

	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].State.LastOutput != output {
		t.Fatalf("expected last output %q, got %q", output, jobs[0].State.LastOutput)
	}
}

func TestCronAddSupportsDelayFlag(t *testing.T) {
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	workspaceDir := t.TempDir()
	flagWorkspace = workspaceDir
	flagConfig = ""

	cmd := newCronAddCmd()
	cmd.SetArgs([]string{"--name", "test-ls", "--delay", "10", "--command", "echo ok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cron add with --delay: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspaceDir, "cron.json")); err == nil {
		t.Fatalf("cron store should not be written to workspace directory %q", workspaceDir)
	}

	storePath := filepath.Join(newHome, ".blades", "cron.json")
	svc := cron.NewService(storePath, nil)
	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	job := jobs[0]
	if job.Schedule.Kind != cron.ScheduleAt {
		t.Fatalf("expected schedule kind %q, got %q", cron.ScheduleAt, job.Schedule.Kind)
	}
	if job.Payload.Kind != cron.PayloadExec {
		t.Fatalf("expected payload kind %q, got %q", cron.PayloadExec, job.Payload.Kind)
	}

	delta := job.Schedule.AtMs - time.Now().UnixMilli()
	if delta < 8000 || delta > 12000 {
		t.Fatalf("expected run time around 10s in the future, got delta=%dms", delta)
	}
}
