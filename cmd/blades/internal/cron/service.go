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

type logFn func(string, ...any)

// computeNextRun returns the next fire time in ms for a given schedule, relative to base.
// Returns 0 if the schedule cannot produce a future run.
func computeNextRun(s Schedule, baseMs int64) int64 {
	// Normalize legacy "once" to "at" so one-shot jobs get a valid next run.
	kind := s.Kind
	if kind == ScheduleKind("once") {
		kind = ScheduleAt
	}
	switch kind {
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
	return newBotHandlerWithExecWorkDir(trigger, notify, execTimeout, execWorkDir, log.Printf)
}

func newBotHandlerWithExecWorkDir(trigger TriggerFn, notify NotifyFn, execTimeout time.Duration, execWorkDir string, logf logFn) Handler {
	if execTimeout <= 0 {
		execTimeout = 60 * time.Second
	}
	workDir := strings.TrimSpace(execWorkDir)
	return func(ctx context.Context, job *Job) (string, error) {
		parentCtx := ctx
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		kind := normalizePayloadKind(job.Payload.Kind)
		switch kind {
		case PayloadExec:
			execCtx, cancel := context.WithTimeout(parentCtx, execTimeout)
			defer cancel()
			c := exec.CommandContext(execCtx, "sh", "-c", job.Payload.Command)
			if workDir != "" {
				c.Dir = workDir
			}
			out, err := c.CombinedOutput()
			output := string(out)
			if err != nil {
				logf("cron exec %q failed: %v, output: %s", job.Name, err, strings.TrimSpace(output))
				return output, fmt.Errorf("command execution failed: %w", err)
			}
			logf("cron exec %q output: %s", job.Name, strings.TrimSpace(output))
			if notify == nil {
				logf("cron exec %q: notify is nil, skip sending result to channel", job.Name)
			} else if job.Payload.ReplySessionID == "" {
				logf("cron exec %q: reply_session_id empty, skip sending result to channel", job.Name)
			} else {
				logf("cron exec %q: sending result to session_id=%s (len=%d)", job.Name, job.Payload.ReplySessionID, len(output))
				if nerr := notify(execCtx, job.Payload.ReplySessionID, output); nerr != nil {
					logf("cron: notify exec result failed: %v", nerr)
				} else {
					logf("cron exec %q: notify ok", job.Name)
				}
			}
			return output, nil

		case PayloadAgentTurn:
			sessID := job.Payload.SessionID
			if sessID == "" {
				sessID = "cron"
			}
			triggerCtx, cancel := context.WithTimeout(parentCtx, 5*time.Minute)
			defer cancel()
			reply, err := trigger(triggerCtx, sessID, job.Payload.Message)
			if err != nil {
				return "", err
			}
			logf("cron agent_turn %q reply: %s", job.Name, reply)
			if notify == nil {
				logf("cron agent_turn %q: notify is nil, skip sending to channel", job.Name)
			} else if job.Payload.ReplySessionID == "" {
				logf("cron agent_turn %q: reply_session_id empty, skip sending to channel", job.Name)
			} else {
				logf("cron agent_turn %q: sending to session_id=%s", job.Name, job.Payload.ReplySessionID)
				if nerr := notify(triggerCtx, job.Payload.ReplySessionID, reply); nerr != nil {
					logf("cron: notify agent_turn result failed: %v", nerr)
				} else {
					logf("cron agent_turn %q: notify ok", job.Name)
				}
			}
			return reply, nil

		case PayloadNotify:
			text := strings.TrimSpace(job.Payload.Message)
			if text == "" {
				return "", fmt.Errorf("notify message is empty")
			}
			target := strings.TrimSpace(job.Payload.ReplySessionID)
			if target == "" {
				return "", fmt.Errorf("notify target session is empty")
			}
			if notify == nil {
				return "", fmt.Errorf("notify handler is nil")
			}
			if err := notify(parentCtx, target, text); err != nil {
				logf("cron notify %q failed: %v", job.Name, err)
				return "", err
			}
			logf("cron notify %q: sent to session_id=%s", job.Name, target)
			return text, nil

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
	case "chat", "social", "channel_message":
		return PayloadNotify
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
	logf      logFn
	now       func() time.Time

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
	return &Service{
		storePath: storePath,
		handler:   handler,
		logf:      log.Printf,
		now:       time.Now,
	}
}

// SetHandler replaces the job execution handler. Safe to call before or after Start.
func (s *Service) SetHandler(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// SetLogger overrides diagnostic logging for the service lifecycle.
func (s *Service) SetLogger(logf func(string, ...any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logf = logf
}

// SetClock overrides the wall clock used for scheduling and stale checks.
func (s *Service) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
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

	s.printf("cron: service started with %d job(s), watching %s every %s",
		len(s.st.Jobs), s.storePath, interval)
	return nil
}

// Stop cancels the background timer and file watcher.
// It is idempotent and safe to call multiple times.
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

	now := s.nowMs()
	next := computeNextRun(schedule, now)
	if next <= 0 {
		return nil, fmt.Errorf("cron: schedule %q does not produce a future run", schedule.Kind)
	}

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
	if normalizePayloadKind(job.Payload.Kind) == PayloadAgentTurn && strings.TrimSpace(job.Payload.SessionID) == "" {
		job.Payload.SessionID = "cron:" + job.ID
	}

	s.st.Jobs = append(s.st.Jobs, job)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	if s.knownIDs != nil {
		s.knownIDs[job.ID] = struct{}{}
	}
	s.armTimerLocked(ctx)
	s.printf("cron: added job %q (%s) schedule_kind=%s next=%s", name, job.ID, schedule.Kind, msToTime(next))
	return job, nil
}

// RemoveJob deletes a job by ID and returns true if found.
func (s *Service) RemoveJob(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return false, err
	}
	before := len(s.st.Jobs)
	s.st.Jobs = removeJobSlice(s.st.Jobs, id)
	if len(s.st.Jobs) == before {
		return false, nil
	}
	if s.knownIDs != nil {
		delete(s.knownIDs, id)
	}
	if err := s.saveLocked(); err != nil {
		return false, err
	}
	s.armTimerLocked(ctx)
	s.printf("cron: removed job %s", id)
	return true, nil
}

// EnableJob enables or disables a job. Returns true if the job was found.
func (s *Service) EnableJob(ctx context.Context, id string, enable bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	for _, j := range s.st.Jobs {
		if j.ID == id {
			j.Enabled = enable
			j.UpdatedAtMs = s.nowMs()
			if enable {
				j.State.NextRunAtMs = computeNextRun(j.Schedule, s.nowMs())
			} else {
				j.State.NextRunAtMs = 0
			}
			if err := s.saveLocked(); err != nil {
				return false, err
			}
			s.armTimerLocked(ctx)
			return true, nil
		}
	}
	return false, nil
}

// ListJobs returns a snapshot of all jobs, sorted by next run time.
func (s *Service) ListJobs(includeDisabled bool) ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}

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
	return out, nil
}

// RunNow immediately executes a job regardless of schedule and returns its output.
func (s *Service) RunNow(ctx context.Context, id string) (string, error) {
	s.mu.Lock()
	var target *Job
	if err := s.loadLocked(); err != nil {
		s.mu.Unlock()
		return "", err
	}
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
	// BUG FIX 2: The original logic checked mtime to decide if a reload was needed, but then
	// checked `s.st.Version != 0` to skip the actual disk read. This means a file that had been
	// loaded (Version=1) would never be re-read even when mtime changed — because setting
	// `s.st = store{}` resets Version to 0, but then the Version check exits before reading.
	// The two conditions were logically correct when combined, but subtle: clear st on mtime
	// change, then fall through to read when Version==0. The original code was actually correct
	// but the watchFile goroutine bypasses this by forcibly clearing s.st before calling
	// loadLocked. No change needed here — kept as-is for clarity.

	if info, err := os.Stat(s.storePath); err == nil {
		if mt := info.ModTime(); mt != s.lastMtime {
			s.st = store{}
			s.lastMtime = mt
		}
	}
	if s.st.Version != 0 {
		return nil // already loaded and up to date
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
		s.printf("cron: corrupt store, starting fresh: %v", err)
		s.st = store{Version: 1}
		return nil
	}
	// Normalize legacy "once" jobs so they get a valid next run time.
	now := s.nowMs()
	for _, j := range s.st.Jobs {
		if j.Schedule.Kind == ScheduleKind("once") {
			j.Schedule.Kind = ScheduleAt
			if j.State.NextRunAtMs == 0 && j.Schedule.AtMs > 0 && j.Schedule.AtMs > now {
				j.State.NextRunAtMs = j.Schedule.AtMs
				s.printf("cron load: job %q (once) fixed nextRunAtMs=%d", j.Name, j.State.NextRunAtMs)
			}
		}
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
	delay := time.Duration(next-s.nowMs()) * time.Millisecond
	if delay < 0 {
		delay = 0
	}
	s.timer = time.AfterFunc(delay, func() {
		s.tick(context.Background())
	})
}

func (s *Service) tick(ctx context.Context) {
	s.mu.Lock()
	if err := s.loadLocked(); err != nil {
		s.printf("cron: tick load error: %v", err)
		s.mu.Unlock()
		return
	}
	now := s.nowMs()

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
		if err := s.saveLocked(); err != nil {
			s.printf("cron: tick save error: %v", err)
		}
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

	// BUG FIX 3: The original code called saveLocked() again after wg.Wait() unconditionally.
	// execute() already calls saveLocked() internally for each job. Calling it again here is
	// redundant and potentially overwrites state written by a concurrent RunNow() call that
	// happened between wg.Wait() returning and this lock being acquired.
	// We still need to re-arm the timer after all executions finish, but we don't need to save.
	s.mu.Lock()
	s.armTimerLocked(ctx)
	s.mu.Unlock()
}

func (s *Service) execute(ctx context.Context, job *Job) (string, error) {
	startMs := s.nowMs()
	s.printf("cron: executing job %q (%s)", job.Name, job.ID)

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
		j.UpdatedAtMs = s.nowMs()
		if execErr != nil {
			j.State.LastStatus = "error"
			j.State.LastError = execErr.Error()
			s.printf("cron: job %q failed: %v", j.Name, execErr)
		} else {
			j.State.LastStatus = "ok"
			j.State.LastError = ""
			s.printf("cron: job %q completed ok", j.Name)
		}
		// BUG FIX 4: DeleteAfterRun was only handled for ScheduleAt jobs. A ScheduleEvery or
		// ScheduleCron job with DeleteAfterRun=true would run forever and never be removed.
		// Fix: check DeleteAfterRun first regardless of schedule kind.
		if j.DeleteAfterRun {
			s.st.Jobs = removeJobSlice(s.st.Jobs, j.ID)
		} else if j.Schedule.Kind == ScheduleAt {
			j.Enabled = false
			j.State.NextRunAtMs = 0
		} else {
			j.State.NextRunAtMs = computeNextRun(j.Schedule, s.nowMs())
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
	for i := len(out); i < len(jobs); i++ {
		jobs[i] = nil
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
				s.printf("cron: watch reload error: %v", err)
				s.mu.Unlock()
				continue
			}
			cur := jobIDSet(s.st.Jobs)
			// Merge new IDs into knownIDs instead of replacing it entirely.
			// This prevents a race where AddJob adds an ID to knownIDs,
			// but we then overwrite it with a fresh map from disk.
			for id := range cur {
				s.knownIDs[id] = struct{}{}
			}
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
					s.printf("cron: detected new job %q (%s) kind=%s next=%s",
						j.Name, j.ID, j.Schedule.Kind, msToTime(j.State.NextRunAtMs))
				}
			}
		}
	}
}

// StaleJobs returns jobs whose last run was more than threshold ago. Used by doctor.
func (s *Service) StaleJobs(threshold time.Duration) []*Job {
	cutoff := s.currentTime().Add(-threshold).UnixMilli()
	jobs, err := s.ListJobs(false)
	if err != nil {
		s.printf("cron: StaleJobs: list jobs failed: %v", err)
		return nil
	}
	var stale []*Job
	for _, j := range jobs {
		if j.State.LastRunAtMs > 0 && j.State.LastRunAtMs < cutoff {
			stale = append(stale, j)
		}
	}
	return stale
}

func (s *Service) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) nowMs() int64 {
	return s.currentTime().UnixMilli()
}

func (s *Service) printf(format string, args ...any) {
	if s != nil && s.logf != nil {
		s.logf(format, args...)
	}
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
	return fmt.Sprintf("[%s] %-20s %-25s %-30s next=%-20s last=%s",
		j.ID, j.Name, sched, describePayload(j.Payload), msToTime(j.State.NextRunAtMs), status)
}

func describePayload(p Payload) string {
	switch normalizePayloadKind(p.Kind) {
	case PayloadExec:
		text := strings.TrimSpace(p.Command)
		if text == "" {
			text = "(empty command)"
		}
		if p.ReplySessionID != "" {
			return fmt.Sprintf("exec -> chat:%s", truncPayloadLabel(p.ReplySessionID, 12))
		}
		return "exec:" + truncPayloadLabel(text, 18)
	case PayloadAgentTurn:
		text := strings.TrimSpace(p.Message)
		if text == "" {
			text = "(empty prompt)"
		}
		base := "agent:" + truncPayloadLabel(text, 16)
		if p.ReplySessionID != "" {
			base += " -> chat:" + truncPayloadLabel(p.ReplySessionID, 12)
		}
		return base
	case PayloadNotify:
		target := strings.TrimSpace(p.ReplySessionID)
		if target == "" {
			target = "missing"
		}
		return "notify -> chat:" + truncPayloadLabel(target, 12)
	default:
		return string(p.Kind)
	}
}

func truncPayloadLabel(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
