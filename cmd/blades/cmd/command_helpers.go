package cmd

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
)

type memoryCommandFunc func(cmd *cobra.Command, mem *memory.Store, args []string) error

type cronCommandFunc func(cmd *cobra.Command, svc *cron.Service, args []string) error

type cronJobSpec struct {
	Name           string
	Schedule       cron.Schedule
	Payload        cron.Payload
	DeleteAfterRun bool
}

func runWithSignalContext(parent context.Context, fn func(context.Context) error) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return fn(ctx)
}

func writeCommandOutput(w io.Writer, output string) {
	if strings.TrimSpace(output) == "" {
		return
	}
	fmt.Fprint(w, output)
	if !strings.HasSuffix(output, "\n") {
		fmt.Fprintln(w)
	}
}

func commandOut(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return io.Discard
	}
	return cmd.OutOrStdout()
}

func commandErr(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return io.Discard
	}
	return cmd.ErrOrStderr()
}

func printCommandf(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(commandOut(cmd), format, args...)
}

func printCommandln(cmd *cobra.Command, args ...any) {
	fmt.Fprintln(commandOut(cmd), args...)
}

func printCommandOutput(cmd *cobra.Command, output string) {
	writeCommandOutput(commandOut(cmd), output)
}

func warnCommandf(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintf(commandErr(cmd), format, args...)
}

func withMemoryStore(run func(*memory.Store) error) error {
	_, _, mem, err := loadAllForOptions(appcore.Options{})
	if err != nil {
		return err
	}
	return run(mem)
}

func withMemoryStoreForOptions(opts appcore.Options, run func(*memory.Store) error) error {
	_, _, mem, err := loadAllForOptions(opts)
	if err != nil {
		return err
	}
	return run(mem)
}

func newMemoryActionCmd(use, short string, args cobra.PositionalArgs, run memoryCommandFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMemoryStoreForOptions(commandOptions(cmd), func(mem *memory.Store) error {
				return run(cmd, mem, args)
			})
		},
	}
}

func withCronService(run func(*cron.Service) error) error {
	svc, err := cronService()
	if err != nil {
		return err
	}
	return run(svc)
}

func withCronServiceForOptions(opts appcore.Options, run func(*cron.Service) error) error {
	svc, err := cronServiceForOptions(opts)
	if err != nil {
		return err
	}
	return run(svc)
}

func newCronServiceCmd(use, short string, args cobra.PositionalArgs, run cronCommandFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withCronServiceForOptions(commandOptions(cmd), func(svc *cron.Service) error {
				return run(cmd, svc, args)
			})
		},
	}
}

func cronPayloadFromFlags(taskType, prompt, command, text, agentSessionID, chatSessionID string) (cron.Payload, error) {
	taskType = strings.ToLower(strings.TrimSpace(taskType))
	prompt = strings.TrimSpace(prompt)
	command = strings.TrimSpace(command)
	text = strings.TrimSpace(text)
	agentSessionID = strings.TrimSpace(agentSessionID)
	chatSessionID = strings.TrimSpace(chatSessionID)

	if taskType == "" {
		switch {
		case command != "":
			taskType = "exec"
		case prompt != "":
			taskType = "agent"
		case text != "":
			taskType = "notify"
		}
	}

	fields := 0
	for _, value := range []string{command, prompt, text} {
		if value != "" {
			fields++
		}
	}
	if fields > 1 {
		return cron.Payload{}, fmt.Errorf("--command, --prompt, and --text are mutually exclusive")
	}

	switch {
	case taskType == "exec":
		if command == "" {
			return cron.Payload{}, fmt.Errorf("--command is required for --type exec")
		}
		return cron.Payload{
			Kind:           cron.PayloadExec,
			Command:        command,
			ReplySessionID: chatSessionID,
		}, nil
	case taskType == "agent":
		if prompt == "" {
			return cron.Payload{}, fmt.Errorf("--prompt is required for --type agent")
		}
		return cron.Payload{
			Kind:           cron.PayloadAgentTurn,
			Message:        prompt,
			SessionID:      agentSessionID,
			ReplySessionID: chatSessionID,
		}, nil
	case taskType == "notify":
		if text == "" {
			return cron.Payload{}, fmt.Errorf("--text is required for --type notify")
		}
		if chatSessionID == "" {
			return cron.Payload{}, fmt.Errorf("--chat-session is required for --type notify")
		}
		return cron.Payload{
			Kind:           cron.PayloadNotify,
			Message:        text,
			ReplySessionID: chatSessionID,
		}, nil
	default:
		return cron.Payload{}, fmt.Errorf("one of --command, --prompt, or --text is required")
	}
}

func findMatchingCronJob(svc *cron.Service, match func(*cron.Job) bool) (*cron.Job, error) {
	jobs, err := svc.ListJobs(true)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if match(job) {
			return job, nil
		}
	}
	return nil, nil
}

func upsertCronJob(ctx context.Context, svc *cron.Service, match func(*cron.Job) bool, spec cronJobSpec) (*cron.Job, bool, error) {
	job, err := findMatchingCronJob(svc, match)
	if err != nil {
		return nil, false, err
	}
	if job != nil {
		if job.Schedule == spec.Schedule && job.Payload == spec.Payload && job.DeleteAfterRun == spec.DeleteAfterRun {
			return job, true, nil
		}
		found, err := svc.RemoveJob(ctx, job.ID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, fmt.Errorf("job %q disappeared during update", job.ID)
		}
	}
	job, err = svc.AddJob(ctx, spec.Name, spec.Schedule, spec.Payload, spec.DeleteAfterRun)
	if err != nil {
		return nil, false, err
	}
	return job, false, nil
}
