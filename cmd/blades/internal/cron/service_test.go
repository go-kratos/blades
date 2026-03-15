package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestNewAgentHandlerWithExecWorkDirUsesWorkDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	h := NewAgentHandlerWithExecWorkDir(nil, time.Second, workDir)
	output, err := h(context.Background(), &Job{
		Payload: Payload{
			Kind:    PayloadExec,
			Command: "pwd",
		},
	})
	if err != nil {
		t.Fatalf("exec with work dir: %v", err)
	}
	if got, want := filepath.Clean(strings.TrimSpace(output)), filepath.Clean(workDir); got != want {
		t.Fatalf("pwd output = %q, want %q", got, want)
	}
}

func TestFormatJobAtPast(t *testing.T) {
	t.Parallel()
	j := &Job{
		ID:       "abc",
		Name:     "ls after 2min",
		Schedule: Schedule{Kind: ScheduleAt, AtMs: 120_000},
		State:    JobState{NextRunAtMs: 0},
	}
	out := FormatJob(j)
	if !strings.Contains(out, "at (past)") {
		t.Errorf("FormatJob with past AtMs should contain %q, got %q", "at (past)", out)
	}
	if strings.Contains(out, "1970-01-01") {
		t.Errorf("FormatJob should not show Unix zero time, got %q", out)
	}
}

func TestServiceTimerFires(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "cron.json")
	var executed atomic.Bool
	handler := func(ctx context.Context, job *Job) (string, error) {
		executed.Store(true)
		return "ok", nil
	}
	svc := NewService(storePath, handler)
	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()
	runAt := time.Now().Add(50 * time.Millisecond).UnixMilli()
	_, err := svc.AddJob(ctx, "soon", Schedule{Kind: ScheduleAt, AtMs: runAt}, Payload{Kind: PayloadExec, Command: "true"}, false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	if !executed.Load() {
		t.Error("handler was not executed within 250ms; timer may not be firing during service run")
	}
}

func TestServiceWatchFileReloadsWhenMtimeUnchanged(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "cron.json")

	seed := map[string]any{
		"version": 1,
		"jobs":    []any{},
	}
	seedData, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(storePath, seedData, 0o644); err != nil {
		t.Fatalf("write seed store: %v", err)
	}
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat seed store: %v", err)
	}
	baseMTime := info.ModTime()

	var executed atomic.Bool
	handler := func(ctx context.Context, job *Job) (string, error) {
		executed.Store(true)
		return "ok", nil
	}

	svc := NewService(storePath, handler)
	svc.WatchInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	now := time.Now().UnixMilli()
	runAt := now + 120
	external := map[string]any{
		"version": 1,
		"jobs": []any{map[string]any{
			"id":      "ext-job-1",
			"name":    "external-job",
			"enabled": true,
			"schedule": map[string]any{
				"kind": "at",
				"atMs": runAt,
			},
			"payload": map[string]any{
				"kind":    "exec",
				"command": "true",
			},
			"state": map[string]any{
				"nextRunAtMs": runAt,
			},
			"createdAtMs":    now,
			"updatedAtMs":    now,
			"deleteAfterRun": false,
		}},
	}
	externalData, err := json.Marshal(external)
	if err != nil {
		t.Fatalf("marshal external store: %v", err)
	}
	if err := os.WriteFile(storePath, externalData, 0o644); err != nil {
		t.Fatalf("write external store: %v", err)
	}
	if err := os.Chtimes(storePath, baseMTime, baseMTime); err != nil {
		t.Fatalf("reset external mtime: %v", err)
	}

	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if executed.Load() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("external job was not executed; watcher likely missed file changes with unchanged mtime")
}
