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

func TestCronCommandsListRemoveAndHeartbeat(t *testing.T) {
	ws := setupCommandWorkspace(t)

	svc := cron.NewService(ws.CronStorePath(), nil)
	job, err := svc.AddJob(context.Background(), "test-job", cron.Schedule{
		Kind:    cron.ScheduleEvery,
		EveryMs: time.Minute.Milliseconds(),
	}, cron.Payload{
		Kind:    cron.PayloadExec,
		Command: "echo ok",
	}, false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	listOut := captureStdout(t, func() {
		cmd := newCronListCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron list: %v", err)
		}
	})
	if !strings.Contains(listOut, "test-job") {
		t.Fatalf("cron list output = %q", listOut)
	}

	removeMissing := newCronRemoveCmd()
	withCommandOptions(removeMissing, workspaceOptions(ws))
	removeMissing.SilenceUsage = true
	removeMissing.SetArgs([]string{"missing-id"})
	if err := removeMissing.Execute(); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected remove missing to fail, got %v", err)
	}

	removeOut := captureStdout(t, func() {
		cmd := newCronRemoveCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{job.ID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron remove: %v", err)
		}
	})
	if !strings.Contains(removeOut, "removed job") {
		t.Fatalf("cron remove output = %q", removeOut)
	}

	emptyOut := captureStdout(t, func() {
		cmd := newCronListCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron list empty: %v", err)
		}
	})
	if !strings.Contains(emptyOut, "(no jobs)") {
		t.Fatalf("cron list empty output = %q", emptyOut)
	}

	heartbeatReady := captureStdout(t, func() {
		cmd := newCronHeartbeatCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{"--every", "15m"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron heartbeat create: %v", err)
		}
	})
	if !strings.Contains(heartbeatReady, "heartbeat job ready") {
		t.Fatalf("heartbeat create output = %q", heartbeatReady)
	}

	heartbeatExisting := captureStdout(t, func() {
		cmd := newCronHeartbeatCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{"--every", "15m"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron heartbeat existing: %v", err)
		}
	})
	if !strings.Contains(heartbeatExisting, "heartbeat job already exists") {
		t.Fatalf("heartbeat existing output = %q", heartbeatExisting)
	}
}

func TestCronAddCommandSupportsMessagePayloadAndDeleteAfterRun(t *testing.T) {
	ws := setupCommandWorkspace(t)

	out := captureStdout(t, func() {
		cmd := newCronAddCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{
			"--name", "daily-brief",
			"--cron", "0 8 * * *",
			"--message", "generate brief",
			"--delete-after-run",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("cron add message payload: %v", err)
		}
	})
	if !strings.Contains(out, "job added") {
		t.Fatalf("cron add output = %q", out)
	}

	svc := cron.NewService(ws.CronStorePath(), nil)
	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Schedule.Kind != cron.ScheduleCron || job.Schedule.Expr != "0 8 * * *" {
		t.Fatalf("schedule = %+v", job.Schedule)
	}
	if job.Payload.Kind != cron.PayloadAgentTurn || job.Payload.Message != "generate brief" {
		t.Fatalf("payload = %+v", job.Payload)
	}
	if !job.DeleteAfterRun {
		t.Fatal("expected DeleteAfterRun to be true")
	}
}

func TestParseScheduleFlagsAdditionalPaths(t *testing.T) {
	schedule, err := parseScheduleFlagsWithNow(func() time.Time {
		return time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	}, "0 8 * * *", "", "", "Asia/Shanghai", scheduleFlagOptions{})
	if err != nil {
		t.Fatalf("parse cron schedule: %v", err)
	}
	if schedule.Kind != cron.ScheduleCron || schedule.Expr != "0 8 * * *" || schedule.TZ != "Asia/Shanghai" {
		t.Fatalf("cron schedule = %+v", schedule)
	}

	every, err := parseScheduleFlagsWithNow(time.Now, "", "5m", "", "", scheduleFlagOptions{})
	if err != nil {
		t.Fatalf("parse every schedule: %v", err)
	}
	if every.Kind != cron.ScheduleEvery || every.EveryMs != (5*time.Minute).Milliseconds() {
		t.Fatalf("every schedule = %+v", every)
	}

	delay, err := parseScheduleFlagsWithNow(func() time.Time {
		return time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	}, "", "", "10", "", scheduleFlagOptions{AllowDelay: true})
	if err != nil {
		t.Fatalf("parse delay schedule: %v", err)
	}
	if delay.Kind != cron.ScheduleAt || delay.AtMs != time.Date(2026, time.March, 18, 9, 0, 10, 0, time.UTC).UnixMilli() {
		t.Fatalf("delay schedule = %+v", delay)
	}

	if _, err := parseScheduleFlags("bad cron", "", "", "", scheduleFlagOptions{}); err == nil || !strings.Contains(err.Error(), "invalid --cron expression") {
		t.Fatalf("expected invalid cron error, got %v", err)
	}
	if _, err := parseScheduleFlags("", "", "10", "", scheduleFlagOptions{}); err == nil || !strings.Contains(err.Error(), "--delay is not supported here") {
		t.Fatalf("expected unsupported delay error, got %v", err)
	}
}

func TestFindHeartbeatJobMatchesTrimmedAgentTurn(t *testing.T) {
	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	_, err := svc.AddJob(context.Background(), "other", cron.Schedule{
		Kind:    cron.ScheduleEvery,
		EveryMs: time.Minute.Milliseconds(),
	}, cron.Payload{
		Kind:    cron.PayloadExec,
		Command: "echo ok",
	}, false)
	if err != nil {
		t.Fatalf("AddJob(other): %v", err)
	}

	want, err := svc.AddJob(context.Background(), " heartbeat ", cron.Schedule{
		Kind:    cron.ScheduleEvery,
		EveryMs: time.Minute.Milliseconds(),
	}, cron.Payload{
		Kind:      cron.PayloadAgentTurn,
		Message:   " heartbeat poll ",
		SessionID: " heartbeat ",
	}, false)
	if err != nil {
		t.Fatalf("AddJob(heartbeat): %v", err)
	}

	got := findHeartbeatJob(svc, "heartbeat", "heartbeat poll", "heartbeat")
	if got == nil || got.ID != want.ID {
		t.Fatalf("findHeartbeatJob() = %+v, want ID %q", got, want.ID)
	}
}

func TestCronHeartbeatRunNowReturnsRuntimeErrorAfterEnsuringJob(t *testing.T) {
	ws := setupCommandWorkspace(t)
	cfgPath := filepath.Join(t.TempDir(), "empty-config.yaml")
	if err := os.WriteFile(cfgPath, []byte("providers: []\n"), 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	cmd := newCronHeartbeatCmd()
	quietCommand(cmd, workspaceOptionsWithConfig(ws, cfgPath))
	cmd.SetArgs([]string{"--run-now"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected heartbeat --run-now to fail without provider config")
	}

	svc := cron.NewService(ws.CronStorePath(), nil)
	job := findHeartbeatJob(svc, defaultHeartbeatJobName, defaultHeartbeatMessage, defaultHeartbeatSessionID)
	if job == nil {
		t.Fatal("expected heartbeat job to be created before runtime failure")
	}
}

func TestCronRunCommandReportsMissingJob(t *testing.T) {
	ws := setupCommandWorkspace(t)
	cfgPath := filepath.Join(t.TempDir(), "provider-config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
`), 0o644); err != nil {
		t.Fatalf("write provider config: %v", err)
	}
	cmd := newCronRunCmd()
	quietCommand(cmd, workspaceOptionsWithConfig(ws, cfgPath))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"missing-job"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing job error, got %v", err)
	}
}
