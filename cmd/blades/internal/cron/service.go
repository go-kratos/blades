package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	robfigcron "github.com/robfig/cron/v3"
)

// nowMs returns the current time as a Unix millisecond timestamp.
func nowMs() int64 { return time.Now().UnixMilli() }

// computeNextRun returns the next fire time in ms for a given schedule, relative to base.
// Returns 0 if the schedule cannot produce a future run.
func computeNextRun(s Schedule, baseMs int64) int64 {
	switch s.Kind {
	case ScheduleAt:
		if s.AtMs > baseMs {
			return s.AtMs
		}
	case ScheduleEvery:
		if s.EveryMs > 0 {
			return baseMs + s.EveryMs
		}
	case ScheduleCron:
		if s.Expr == "" {
			return 0
		}
		loc := time.Local
		if s.TZ != "" {
			if l, err := time.LoadLocation(s.TZ); err == nil {
				loc = l
			} else {
				log.Printf("cron: invalid timezone %q, falling back to local: %v", s.TZ, err)
			}
		}
		parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
		sched, err := parser.Parse(s.Expr)
		if err != nil {
			log.Printf("cron: invalid cron expression %q: %v", s.Expr, err)
			return 0
		}
		return sched.Next(time.UnixMilli(baseMs).In(loc)).UnixMilli()
	}
	return 0
}

// Handler is called whenever a job fires. The returned string is stored as LastOutput.
// Return a non-nil error to mark the job as failed.
type Handler func(ctx context.Context, job *Job) (output string, err error)

// TriggerFn injects a user message into the agent and returns its reply.
type TriggerFn func(ctx context.Context, sessionID, text string) (string, error)

// NotifyFn sends text directly to a channel session without involving the LLM.
type NotifyFn func(ctx context.Context, sessionID, text string) error

// NewBotHandler returns a Handler that dispatches PayloadExec and PayloadAgentTurn jobs.
// When notify is non-nil and the job has ReplySessionID, the output is forwarded to that session.
// execTimeout <= 0 defaults to 60s.
func NewBotHandler(trigger TriggerFn, notify NotifyFn, execTimeout time.Duration) Handler {
	return NewBotHandlerWithExecWorkDir(trigger, notify, execTimeout, "")
}

// NewBotHandlerWithExecWorkDir is like NewBotHandler but runs PayloadExec
// commands in execWorkDir when it is non-empty.
func NewBotHandlerWithExecWorkDir(trigger TriggerFn, notify NotifyFn, execTimeout time.Duration, execWorkDir string) Handler {
	if execTimeout <= 0 {
		execTimeout = 60 * time.Second
	}
	workDir := strings.TrimSpace(execWorkDir)
	return func(ctx context.Context, job *Job) (string, error) {
		kind := normalizePayloadKind(job.Payload.Kind)
		switch kind {
		case PayloadExec:
			execCtx, cancel := context.WithTimeout(context.Background(), execTimeout)
			defer cancel()
			c := exec.CommandContext(execCtx, "sh", "-c", job.Payload.Command)
			if workDir != "" {
				c.Dir = workDir
			}
			out, err := c.CombinedOutput()
			output := string(out)
			if err != nil {
				log.Printf("cron exec %q failed: %v, output: %s", job.Name, err, strings.TrimSpace(output))
				return output, fmt.Errorf("command execution failed: %w", err)
			}
			log.Printf("cron exec %q output: %s", job.Name, strings.TrimSpace(output))
			if notify != nil && job.Payload.ReplySessionID != "" {
				if nerr := notify(ctx, job.Payload.ReplySessionID, output); nerr != nil {
					log.Printf("cron: notify exec result failed: %v", nerr)
				}
			}
			return output, nil

		case PayloadAgentTurn:
			sessID := job.Payload.SessionID
			if sessID == "" {
				sessID = "cron"
			}
			triggerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			reply, err := trigger(triggerCtx, sessID, job.Payload.Message)
			if err != nil {
				return "", err
			}
			log.Printf("cron agent_turn %q reply: %s", job.Name, reply)
			if notify != nil && job.Payload.ReplySessionID != "" {
				if nerr := notify(ctx, job.Payload.ReplySessionID, reply); nerr != nil {
					log.Printf("cron: notify agent_turn result failed: %v", nerr)
				}
			}
			return reply, nil

		default:
			return "", fmt.Errorf("unknown payload kind %q", job.Payload.Kind)
		}
	}
}

// NewAgentHandler returns a Handler that executes PayloadExec and PayloadAgentTurn jobs.
// It is equivalent to NewBotHandler(trigger, nil, execTimeout) for backward compatibility.
func NewAgentHandler(trigger TriggerFn, execTimeout time.Duration) Handler {
	return NewBotHandler(trigger, nil, execTimeout)
}

// NewAgentHandlerWithExecWorkDir is like NewAgentHandler but runs PayloadExec
// commands in execWorkDir when it is non-empty.
func NewAgentHandlerWithExecWorkDir(trigger TriggerFn, execTimeout time.Duration, execWorkDir string) Handler {
	return NewBotHandlerWithExecWorkDir(trigger, nil, execTimeout, execWorkDir)
}

func normalizePayloadKind(kind PayloadKind) PayloadKind {
	switch strings.ToLower(strings.TrimSpace(string(kind))) {
	case "shell", "command":
		return PayloadExec
	case "message", "agent_message":
		return PayloadAgentTurn
	default:
		return kind
	}
}

// DefaultExecHandler returns a Handler that runs only PayloadExec jobs (shell commands)
// in the given workDir. PayloadAgentTurn jobs are skipped; use NewBotHandler for both.
func DefaultExecHandler(timeout time.Duration, workDir ...string) Handler {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	var dir string
	if len(workDir) > 0 {
		dir = strings.TrimSpace(workDir[0])
	}
	return func(ctx context.Context, job *Job) (string, error) {
		if job.Payload.Kind != PayloadExec {
			return "", nil
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		c := exec.CommandContext(ctx, "sh", "-c", job.Payload.Command)
		if dir != "" {
			c.Dir = dir
		}
		out, err := c.CombinedOutput()
		return string(out), err
	}
}

// Service manages a set of persistent scheduled jobs.
type Service struct {
	storePath string
	handler   Handler

	// WatchInterval is how often the store file is polled for external changes. Defaults to 5s.
	WatchInterval time.Duration

	mu        sync.Mutex
	st        store
	lastMtime time.Time
	knownIDs  map[string]struct{}
	timer     *time.Timer
	watchStop chan struct{}
	running   bool
}

// NewService creates a Service that persists jobs to storePath.
// handler may be nil; use SetHandler to assign or replace it.
func NewService(storePath string, handler Handler) *Service {
	return &Service{storePath: storePath, handler: handler}
}

// SetHandler replaces the job execution handler. Safe to call before or after Start.
func (s *Service) SetHandler(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// Start loads the persisted store, rearms the timer, and marks the service running.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return fmt.Errorf("cron load: %w", err)
	}
	s.running = true
	s.knownIDs = jobIDSet(s.st.Jobs)
	s.armTimerLocked(ctx)

	s.watchStop = make(chan struct{})
	interval := s.WatchInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go s.watchFile(ctx, interval)

	log.Printf("cron: service started with %d job(s), watching %s every %s",
		len(s.st.Jobs), s.storePath, interval)
	return nil
}

// Stop cancels the background timer and file watcher.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.watchStop != nil {
		close(s.watchStop)
		s.watchStop = nil
	}
}

// AddJob creates a new job and persists it.
func (s *Service) AddJob(ctx context.Context, name string, schedule Schedule, payload Payload, deleteAfterRun bool) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	now := nowMs()
	next := computeNextRun(schedule, now)

	job := &Job{
		ID:             uuid.New().String()[:8],
		Name:           name,
		Enabled:        true,
		Schedule:       schedule,
		Payload:        payload,
		State:          JobState{NextRunAtMs: next},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	s.st.Jobs = append(s.st.Jobs, job)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	if s.knownIDs != nil {
		s.knownIDs[job.ID] = struct{}{}
	}
	s.armTimerLocked(ctx)
	log.Printf("cron: added job %q (%s) next=%s", name, job.ID, msToTime(next))
	return job, nil
}

// RemoveJob deletes a job by ID and returns true if found.
func (s *Service) RemoveJob(ctx context.Context, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.loadLocked()
	before := len(s.st.Jobs)
	s.st.Jobs = removeJobSlice(s.st.Jobs, id)
	if len(s.st.Jobs) == before {
		return false
	}
	if s.knownIDs != nil {
		delete(s.knownIDs, id)
	}
	_ = s.saveLocked()
	s.armTimerLocked(ctx)
	log.Printf("cron: removed job %s", id)
	return true
}

// EnableJob enables or disables a job. Returns true if the job was found.
func (s *Service) EnableJob(ctx context.Context, id string, enable bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	for _, j := range s.st.Jobs {
		if j.ID == id {
			j.Enabled = enable
			j.UpdatedAtMs = nowMs()
			if enable {
				j.State.NextRunAtMs = computeNextRun(j.Schedule, nowMs())
			} else {
				j.State.NextRunAtMs = 0
			}
			_ = s.saveLocked()
			s.armTimerLocked(ctx)
			return true
		}
	}
	return false
}

// ListJobs returns a snapshot of all jobs, sorted by next run time.
func (s *Service) ListJobs(includeDisabled bool) []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()

	out := make([]*Job, 0, len(s.st.Jobs))
	for _, j := range s.st.Jobs {
		if !includeDisabled && !j.Enabled {
			continue
		}
		cp := *j
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, k int) bool {
		a, b := out[i].State.NextRunAtMs, out[k].State.NextRunAtMs
		if a == 0 {
			return false
		}
		if b == 0 {
			return true
		}
		return a < b
	})
	return out
}

// RunNow immediately executes a job regardless of schedule and returns its output.
func (s *Service) RunNow(ctx context.Context, id string) (string, error) {
	s.mu.Lock()
	var target *Job
	_ = s.loadLocked()
	for _, j := range s.st.Jobs {
		if j.ID == id {
			cp := *j
			target = &cp
			break
		}
	}
	s.mu.Unlock()

	if target == nil {
		return "", fmt.Errorf("cron: job %q not found", id)
	}
	return s.execute(ctx, target)
}

// ---- internal ---------------------------------------------------------------

func (s *Service) loadLocked() error {
	if info, err := os.Stat(s.storePath); err == nil {
		if mt := info.ModTime(); mt != s.lastMtime {
			s.st = store{}
			s.lastMtime = mt
		}
	}
	if s.st.Version != 0 {
		return nil // already loaded
	}

	data, err := os.ReadFile(s.storePath)
	if os.IsNotExist(err) {
		s.st = store{Version: 1}
		return nil
	}
	if err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		s.st = store{Version: 1}
		return nil
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		log.Printf("cron: corrupt store, starting fresh: %v", err)
		s.st = store{Version: 1}
	}
	return nil
}

func (s *Service) saveLocked() error {
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.st); err != nil {
		return err
	}
	// Atomic write: write to temp file then rename to avoid partial reads.
	tmp, err := os.CreateTemp(dir, ".cron-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, s.storePath); err != nil {
		os.Remove(tmpName)
		return err
	}
	if info, err := os.Stat(s.storePath); err == nil {
		s.lastMtime = info.ModTime()
	}
	return nil
}

func (s *Service) nextWakeLocked() int64 {
	var min int64
	for _, j := range s.st.Jobs {
		if !j.Enabled || j.State.NextRunAtMs == 0 {
			continue
		}
		if min == 0 || j.State.NextRunAtMs < min {
			min = j.State.NextRunAtMs
		}
	}
	return min
}

func (s *Service) armTimerLocked(ctx context.Context) {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.running {
		return
	}
	next := s.nextWakeLocked()
	if next == 0 {
		return
	}
	delay := time.Duration(next-nowMs()) * time.Millisecond
	if delay < 0 {
		delay = 0
	}
	s.timer = time.AfterFunc(delay, func() {
		s.tick(context.Background())
	})
}

func (s *Service) tick(ctx context.Context) {
	s.mu.Lock()
	_ = s.loadLocked()
	now := nowMs()

	var due []*Job
	for _, j := range s.st.Jobs {
		if j.Enabled && j.State.NextRunAtMs > 0 && now >= j.State.NextRunAtMs {
			cp := *j
			due = append(due, &cp)
		}
	}
	// Pre-advance schedules before releasing the lock so watchFile does not re-queue the same job.
	if len(due) > 0 {
		dueSet := make(map[string]bool, len(due))
		for _, d := range due {
			dueSet[d.ID] = true
		}
		for _, j := range s.st.Jobs {
			if !dueSet[j.ID] {
				continue
			}
			if j.Schedule.Kind == ScheduleAt {
				j.Enabled = false
				j.State.NextRunAtMs = 0
			} else {
				j.State.NextRunAtMs = computeNextRun(j.Schedule, now)
			}
		}
		_ = s.saveLocked()
	}
	s.mu.Unlock()

	// Execute due jobs concurrently so one slow job does not block others.
	var wg sync.WaitGroup
	for _, j := range due {
		wg.Add(1)
		go func(job *Job) {
			defer wg.Done()
			_, _ = s.execute(ctx, job)
		}(j)
	}
	wg.Wait()

	s.mu.Lock()
	_ = s.saveLocked()
	s.armTimerLocked(ctx)
	s.mu.Unlock()
}

func (s *Service) execute(ctx context.Context, job *Job) (string, error) {
	startMs := nowMs()
	log.Printf("cron: executing job %q (%s)", job.Name, job.ID)

	var output string
	var execErr error
	if s.handler != nil {
		output, execErr = s.handler(ctx, job)
	}

	// Persist state to disk so RunNow callers see updated LastOutput/LastStatus.

	s.mu.Lock()
	for _, j := range s.st.Jobs {
		if j.ID != job.ID {
			continue
		}
		j.State.LastRunAtMs = startMs
		j.State.LastOutput = output
		j.UpdatedAtMs = nowMs()
		if execErr != nil {
			j.State.LastStatus = "error"
			j.State.LastError = execErr.Error()
			log.Printf("cron: job %q failed: %v", j.Name, execErr)
		} else {
			j.State.LastStatus = "ok"
			j.State.LastError = ""
			log.Printf("cron: job %q completed ok", j.Name)
		}
		if j.Schedule.Kind == ScheduleAt {
			if j.DeleteAfterRun {
				s.st.Jobs = removeJobSlice(s.st.Jobs, j.ID)
			} else {
				j.Enabled = false
				j.State.NextRunAtMs = 0
			}
		} else {
			j.State.NextRunAtMs = computeNextRun(j.Schedule, nowMs())
		}
		break
	}
	_ = s.saveLocked()
	s.mu.Unlock()
	return output, execErr
}

func removeJobSlice(jobs []*Job, id string) []*Job {
	out := jobs[:0]
	for _, j := range jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return out
}

func msToTime(ms int64) string {
	if ms == 0 {
		return "never"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func jobIDSet(jobs []*Job) map[string]struct{} {
	m := make(map[string]struct{}, len(jobs))
	for _, j := range jobs {
		m[j.ID] = struct{}{}
	}
	return m
}

// watchFile polls the store file on interval and reloads when modified externally.
func (s *Service) watchFile(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Capture stop channel while holding the lock to avoid race with Stop().
	s.mu.Lock()
	stop := s.watchStop
	s.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			prev := make(map[string]struct{}, len(s.knownIDs))
			for id := range s.knownIDs {
				prev[id] = struct{}{}
			}

			// Force disk reload on each tick so external edits are picked up even
			// on filesystems with coarse mtime granularity.
			s.st = store{}
			if err := s.loadLocked(); err != nil {
				log.Printf("cron: watch reload error: %v", err)
				s.mu.Unlock()
				continue
			}
			cur := jobIDSet(s.st.Jobs)
			s.knownIDs = cur
			s.armTimerLocked(ctx) // re-arm after any reload so external edits to next run take effect
			jobs := make([]Job, 0, len(s.st.Jobs))
			for _, j := range s.st.Jobs {
				if j != nil {
					jobs = append(jobs, *j)
				}
			}
			s.mu.Unlock()

			for _, j := range jobs {
				if _, existed := prev[j.ID]; !existed {
					log.Printf("cron: detected new job %q (%s) kind=%s next=%s",
						j.Name, j.ID, j.Schedule.Kind, msToTime(j.State.NextRunAtMs))
				}
			}
		}
	}
}

// StaleJobs returns jobs whose last run was more than threshold ago. Used by doctor.
func (s *Service) StaleJobs(threshold time.Duration) []*Job {
	cutoff := time.Now().Add(-threshold).UnixMilli()
	jobs := s.ListJobs(false)
	var stale []*Job
	for _, j := range jobs {
		if j.State.LastRunAtMs > 0 && j.State.LastRunAtMs < cutoff {
			stale = append(stale, j)
		}
	}
	return stale
}

// FormatJob returns a human-readable one-line description of a job.
func FormatJob(j *Job) string {
	var sched string
	switch j.Schedule.Kind {
	case ScheduleAt:
		if j.Schedule.AtMs <= 0 {
			sched = "at (never)"
		} else if j.Schedule.AtMs < nowMs() {
			sched = "at (past)"
		} else {
			sched = fmt.Sprintf("at %s", msToTime(j.Schedule.AtMs))
		}
	case ScheduleEvery:
		sched = fmt.Sprintf("every %s", time.Duration(j.Schedule.EveryMs)*time.Millisecond)
	case ScheduleCron:
		sched = fmt.Sprintf("cron(%s)", j.Schedule.Expr)
	}
	status := j.State.LastStatus
	if status == "" {
		status = "pending"
	}
	return fmt.Sprintf("[%s] %-20s %-25s next=%-20s last=%s",
		j.ID, j.Name, sched, msToTime(j.State.NextRunAtMs), status)
}
