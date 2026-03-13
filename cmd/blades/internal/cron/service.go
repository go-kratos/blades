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

func nowMs() int64 { return time.Now().UnixMilli() }

func debugEnabled() bool { return os.Getenv("BLADES_DEBUG") == "1" }

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
		parser := robfigcron.NewParser(
			robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow,
		)
		sched, err := parser.Parse(s.Expr)
		if err != nil {
			log.Printf("cron: invalid expr %q: %v", s.Expr, err)
			return 0
		}
		return sched.Next(time.UnixMilli(baseMs).In(loc)).UnixMilli()
	}
	return 0
}

// Handler is called whenever a job fires.
type Handler func(ctx context.Context, job *Job) (output string, err error)

// TriggerFn injects a user message into the agent and returns the reply.
type TriggerFn func(ctx context.Context, sessionID, text string) (string, error)

// NewAgentHandler returns a Handler that executes PayloadExec and PayloadAgentTurn jobs.
func NewAgentHandler(trigger TriggerFn, execTimeout time.Duration) Handler {
	if execTimeout <= 0 {
		execTimeout = 60 * time.Second
	}
	return func(ctx context.Context, job *Job) (string, error) {
		switch job.Payload.Kind {
		case PayloadExec:
			ec, cancel := context.WithTimeout(context.Background(), execTimeout)
			defer cancel()
			out, err := exec.CommandContext(ec, "sh", "-c", job.Payload.Command).CombinedOutput()
			return string(out), err

		case PayloadAgentTurn:
			sessID := job.Payload.SessionID
			if sessID == "" {
				sessID = "cron"
			}
			tc, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			return trigger(tc, sessID, job.Payload.Message)

		default:
			return "", fmt.Errorf("unknown payload kind %q", job.Payload.Kind)
		}
	}
}

// Service manages a set of persistent scheduled jobs.
type Service struct {
	storePath     string
	handler       Handler
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
func NewService(storePath string, handler Handler) *Service {
	return &Service{storePath: storePath, handler: handler}
}

// SetHandler replaces the job execution handler.
func (s *Service) SetHandler(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// Start loads jobs, arms the timer, and begins polling.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return fmt.Errorf("cron: load: %w", err)
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
	log.Printf("cron: started with %d job(s)", len(s.st.Jobs))
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

// AddJob creates and persists a new job.
func (s *Service) AddJob(ctx context.Context, name string, schedule Schedule, payload Payload, deleteAfterRun bool) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	now := nowMs()
	job := &Job{
		ID:             uuid.New().String()[:8],
		Name:           name,
		Enabled:        true,
		Schedule:       schedule,
		Payload:        payload,
		State:          JobState{NextRunAtMs: computeNextRun(schedule, now)},
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
	log.Printf("cron: added job %q (%s)", name, job.ID)
	return job, nil
}

// RemoveJob deletes a job by ID.
func (s *Service) RemoveJob(ctx context.Context, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.loadLocked()
	before := len(s.st.Jobs)
	filtered := s.st.Jobs[:0]
	for _, j := range s.st.Jobs {
		if j.ID != id {
			filtered = append(filtered, j)
		}
	}
	s.st.Jobs = filtered
	if len(s.st.Jobs) == before {
		return false
	}
	if s.knownIDs != nil {
		delete(s.knownIDs, id)
	}
	_ = s.saveLocked()
	s.armTimerLocked(ctx)
	return true
}

// ListJobs returns a sorted snapshot of jobs.
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

// RunNow immediately executes a job and returns its output.
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

// ─── internal ────────────────────────────────────────────────────────────────

func (s *Service) loadLocked() error {
	if info, err := os.Stat(s.storePath); err == nil {
		if mt := info.ModTime(); mt != s.lastMtime {
			s.st = store{}
			s.lastMtime = mt
		}
	}
	if s.st.Version != 0 {
		return nil
	}
	data, err := os.ReadFile(s.storePath)
	if os.IsNotExist(err) {
		s.st = store{Version: 1}
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		log.Printf("cron: corrupt store, starting fresh: %v", err)
		s.st = store{Version: 1}
	}
	return nil
}

func (s *Service) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.storePath), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.st); err != nil {
		return err
	}
	if err := os.WriteFile(s.storePath, buf.Bytes(), 0o644); err != nil {
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
	s.timer = time.AfterFunc(delay, func() { s.tick(context.Background()) })
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
	s.mu.Unlock()

	for _, j := range due {
		_, _ = s.execute(ctx, j)
	}

	s.mu.Lock()
	_ = s.saveLocked()
	s.armTimerLocked(ctx)
	s.mu.Unlock()
}

func (s *Service) execute(ctx context.Context, job *Job) (string, error) {
	startMs := nowMs()
	if debugEnabled() {
		log.Printf("cron: executing job %q (%s)", job.Name, job.ID)
	}

	var output string
	var execErr error
	if s.handler != nil {
		output, execErr = s.handler(ctx, job)
	}
	trimmedOutput := strings.TrimSpace(output)
	if debugEnabled() && trimmedOutput != "" {
		log.Printf("cron: output for job %q (%s):\n%s", job.Name, job.ID, trimmedOutput)
	}

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
		} else {
			j.State.LastStatus = "ok"
			j.State.LastError = ""
		}
		if j.Schedule.Kind == ScheduleAt {
			if j.DeleteAfterRun {
				s.st.Jobs = removeJob(s.st.Jobs, j.ID)
			} else {
				j.Enabled = false
				j.State.NextRunAtMs = 0
			}
		} else {
			j.State.NextRunAtMs = computeNextRun(j.Schedule, nowMs())
		}
		break
	}
	s.mu.Unlock()
	return output, execErr
}

func removeJob(jobs []*Job, id string) []*Job {
	out := jobs[:0]
	for _, j := range jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return out
}

func jobIDSet(jobs []*Job) map[string]struct{} {
	m := make(map[string]struct{}, len(jobs))
	for _, j := range jobs {
		m[j.ID] = struct{}{}
	}
	return m
}

func msToTime(ms int64) string {
	if ms == 0 {
		return "never"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func (s *Service) watchFile(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	stop := s.watchStop

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			prev := s.knownIDs
			old := s.st.Version // force reload by resetting
			s.st.Version = 0
			_ = s.loadLocked()
			cur := jobIDSet(s.st.Jobs)
			s.knownIDs = cur
			_ = old
			for id := range cur {
				if _, existed := prev[id]; !existed {
					log.Printf("cron: detected new job %s from external edit", id)
				}
			}
			s.armTimerLocked(ctx)
			s.mu.Unlock()
		}
	}
}

// StaleJobs returns jobs whose last run was more than threshold ago (or never ran and
// are expected to run). Used by doctor to surface stuck jobs.
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
		sched = fmt.Sprintf("at %s", msToTime(j.Schedule.AtMs))
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
