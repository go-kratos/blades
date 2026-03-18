package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

func TestCronToolAddAcceptsLegacyShellPayloadKind(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	tool := &cronTool{svc: svc}

	result, err := tool.add(context.Background(), cronInput{
		Name: "exec-task",
		Schedule: &cronScheduleInput{
			Type: "at",
			AtMs: time.Now().Add(time.Minute).UnixMilli(),
		},
		Task: &cronTaskInput{
			Type:    "exec",
			Command: "ls .",
		},
	})
	if err != nil {
		t.Fatalf("add exec task: %v", err)
	}
	if !strings.Contains(result, "Job added.") {
		t.Fatalf("unexpected add result %q", result)
	}

	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
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

func TestCronToolRunAcceptsJobID(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), func(ctx context.Context, job *cron.Job) (string, error) {
		return "ok", nil
	})
	tool := &cronTool{svc: svc}

	job, err := svc.AddJob(context.Background(), "run-alias", cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: 1000}, cron.Payload{Kind: cron.PayloadExec, Command: "true"}, false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	for _, in := range []map[string]any{
		{"action": "run", "job_id": job.ID},
		{"action": "run", "name": job.Name},
	} {
		lastRunBefore := job.State.LastRunAtMs
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		out, err := tool.handle(context.Background(), string(raw))
		if err != nil {
			t.Fatalf("run with input %v: %v", in, err)
		}
		if !strings.Contains(out, "triggered") {
			t.Fatalf("unexpected output for input %v: %q", in, out)
		}

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			jobs, err := svc.ListJobs(true)
			if err != nil {
				t.Fatalf("ListJobs after run: %v", err)
			}
			if len(jobs) == 1 && jobs[0].State.LastRunAtMs > lastRunBefore {
				job = jobs[0]
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if job.State.LastRunAtMs <= lastRunBefore {
			t.Fatalf("job %q did not finish running for input %v", job.ID, in)
		}
	}
}

func TestCronToolRemoveSupportsName(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	tool := &cronTool{svc: svc}

	job, err := svc.AddJob(context.Background(), "delete-me", cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: 1000}, cron.Payload{Kind: cron.PayloadExec, Command: "true"}, false)
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}

	out, err := tool.handle(context.Background(), `{"action":"remove","name":"delete-me"}`)
	if err != nil {
		t.Fatalf("remove by name: %v", err)
	}
	if !strings.Contains(out, job.ID) {
		t.Fatalf("remove output %q does not include removed job id %q", out, job.ID)
	}

	jobs, err := svc.ListJobs(true)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after remove by name, got %d", len(jobs))
	}
}

func TestCronToolRunWithoutIdentifierReturnsHelpfulError(t *testing.T) {
	t.Parallel()

	svc := cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil)
	tool := &cronTool{svc: svc}

	_, err := tool.handle(context.Background(), `{"action":"run"}`)
	if err == nil {
		t.Fatal("expected error for missing identifier")
	}
	if !strings.Contains(err.Error(), "job_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("error should describe accepted identifiers, got: %v", err)
	}
}

func TestNewCronToolSchemaIncludesActionRulesAndHints(t *testing.T) {
	t.Parallel()

	tool := NewCronTool(cron.NewService(filepath.Join(t.TempDir(), "cron.json"), nil))
	schema := tool.InputSchema()
	if schema == nil {
		t.Fatal("expected input schema")
	}
	if !strings.Contains(schema.Description, "Prefer delay_seconds") {
		t.Fatalf("schema description = %q", schema.Description)
	}

	action := schema.Properties["action"]
	if action == nil {
		t.Fatal("expected action property")
	}
	if got, want := action.Enum, []any{"add", "list", "remove", "run"}; !slices.Equal(got, want) {
		t.Fatalf("action enum = %v, want %v", got, want)
	}

	schedule := schema.Properties["schedule"]
	if schedule == nil || schedule.Properties == nil {
		t.Fatalf("expected schedule property, got %+v", schedule)
	}
	if got, want := schedule.Properties["type"].Enum, []any{"at", "every", "cron"}; !slices.Equal(got, want) {
		t.Fatalf("schedule.type enum = %v, want %v", got, want)
	}

	task := schema.Properties["task"]
	if task == nil || task.Properties == nil {
		t.Fatalf("expected task property, got %+v", task)
	}
	if got, want := task.Properties["type"].Enum, []any{"exec", "agent", "notify"}; !slices.Equal(got, want) {
		t.Fatalf("task.type enum = %v, want %v", got, want)
	}
	if prop := schedule.Properties["delay_seconds"]; prop == nil || prop.ExclusiveMinimum == nil || *prop.ExclusiveMinimum != 0 {
		t.Fatalf("schedule.delay_seconds should be > 0, got %+v", prop)
	}
}
