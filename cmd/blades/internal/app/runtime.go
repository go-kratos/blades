package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/logger"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

type Runtime struct {
	Config    *config.Config
	Workspace *workspace.Workspace
	Memory    *memory.Store
	Sessions  *session.Manager
	Cron      *cron.Service
	Runner    *blades.Runner
}

type TurnOptions struct {
	Writer          channel.Writer
	Memory          *memory.Store
	LogConversation bool
	RuntimeLog      *logger.Runtime
}

type TurnExecutor struct {
	Runner   *blades.Runner
	Sessions *session.Manager
	Options  TurnOptions
}

type streamCollector struct {
	writer       channel.Writer
	buf          strings.Builder
	startedTools map[string]bool
	endedTools   map[string]bool
}

type turnRecorder struct {
	memory          *memory.Store
	logConversation bool
	runtimeLog      *logger.Runtime
}

func BuildRuntime(cfg *config.Config, ws *workspace.Workspace, mem *memory.Store) (*Runtime, error) {
	sessMgr, err := BuildSessionManager(cfg, ws)
	if err != nil {
		return nil, err
	}
	cronSvc := cron.NewService(ws.CronStorePath(), nil)
	runner, err := BuildRunner(cfg, ws, cronSvc)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Config:    cfg,
		Workspace: ws,
		Memory:    mem,
		Sessions:  sessMgr,
		Cron:      cronSvc,
		Runner:    runner,
	}, nil
}

func ConfigureRuntimeCron(rt *Runtime, notify cron.NotifyFn) {
	if rt == nil || rt.Cron == nil {
		return
	}
	rt.Cron.SetHandler(cron.NewBotHandlerWithExecWorkDir(
		NewTurnExecutor(rt.Runner, rt.Sessions, TurnOptions{}).Trigger(),
		notify,
		60*time.Second,
		DefaultExecWorkingDir(rt.Workspace),
	))
}

func ToolEventKey(tp blades.ToolPart, ordinal int) string {
	if strings.TrimSpace(tp.ID) != "" {
		return tp.ID
	}
	// Requests can stream in incrementally, so they are not stable enough to
	// identify a single tool invocation. Fall back to tool order instead.
	return tp.Name + "\n#" + strconv.Itoa(ordinal)
}

func NewTurnExecutor(runner *blades.Runner, sessMgr *session.Manager, opts TurnOptions) *TurnExecutor {
	return &TurnExecutor{
		Runner:   runner,
		Sessions: sessMgr,
		Options:  opts,
	}
}

func (e *TurnExecutor) Run(ctx context.Context, sessionID, text string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("no turn executor")
	}
	return runTurn(ctx, e.Runner, e.Sessions, sessionID, text, e.Options)
}

func (e *TurnExecutor) StreamHandler() channel.StreamHandler {
	return func(ctx context.Context, sid, text string, w channel.Writer) (string, error) {
		opts := e.Options
		opts.Writer = w
		return runTurn(ctx, e.Runner, e.Sessions, sid, text, opts)
	}
}

func (e *TurnExecutor) Trigger() cron.TriggerFn {
	return func(ctx context.Context, sessionID, text string) (string, error) {
		opts := e.Options
		opts.Writer = nil
		return runTurn(ctx, e.Runner, e.Sessions, sessionID, text, opts)
	}
}

func newStreamCollector(writer channel.Writer) *streamCollector {
	return &streamCollector{
		writer:       writer,
		startedTools: make(map[string]bool),
		endedTools:   make(map[string]bool),
	}
}

func (c *streamCollector) reply() string {
	return c.buf.String()
}

func (c *streamCollector) consume(m *blades.Message) {
	if m == nil {
		return
	}

	if m.Status == blades.StatusCompleted {
		finalText := m.Text()
		if c.buf.Len() == 0 && finalText != "" && c.writer != nil {
			c.writer.WriteText(finalText)
		}
		if finalText != "" {
			c.buf.Reset()
			c.buf.WriteString(finalText)
		}
	} else if chunk := m.Text(); chunk != "" {
		if c.writer != nil {
			c.writer.WriteText(chunk)
		}
		c.buf.WriteString(chunk)
	}

	if c.writer == nil {
		return
	}
	c.emitToolEvents(m)
}

func (c *streamCollector) emitToolEvents(m *blades.Message) {
	toolOrdinals := make(map[string]int)
	for _, part := range m.Parts {
		tp, ok := part.(blades.ToolPart)
		if !ok {
			continue
		}

		fingerprint := tp.Name + "\n" + tp.Request
		toolOrdinals[fingerprint]++
		key := ToolEventKey(tp, toolOrdinals[fingerprint])
		if !tp.Completed {
			if !c.startedTools[key] {
				c.startedTools[key] = true
				c.writer.WriteEvent(channel.Event{
					Kind:  channel.EventToolStart,
					ID:    key,
					Name:  tp.Name,
					Input: tp.Request,
				})
			}
			continue
		}
		if c.endedTools[key] {
			continue
		}
		if !c.startedTools[key] {
			c.startedTools[key] = true
			c.writer.WriteEvent(channel.Event{
				Kind:  channel.EventToolStart,
				ID:    key,
				Name:  tp.Name,
				Input: tp.Request,
			})
		}
		c.endedTools[key] = true
		c.writer.WriteEvent(channel.Event{
			Kind:   channel.EventToolEnd,
			ID:     key,
			Name:   tp.Name,
			Input:  tp.Request,
			Output: tp.Response,
		})
	}
}

func newTurnRecorder(opts TurnOptions) turnRecorder {
	return turnRecorder{
		memory:          opts.Memory,
		logConversation: opts.LogConversation,
		runtimeLog:      opts.RuntimeLog,
	}
}

func (r turnRecorder) recordUser(sessionID, text string) {
	if r.runtimeLog != nil {
		r.runtimeLog.WriteConversation(sessionID, "user", text)
	}
}

func (r turnRecorder) recordError(sessionID string, err error) {
	if err != nil && r.runtimeLog != nil {
		r.runtimeLog.WriteConversation(sessionID, "assistant_error", err.Error())
	}
}

func (r turnRecorder) recordAssistant(sessionID, userText, reply string) {
	if r.runtimeLog != nil {
		r.runtimeLog.WriteConversation(sessionID, "assistant", reply)
	}
	if r.logConversation && r.memory != nil {
		_ = r.memory.AppendDailyLog("user", userText)
		_ = r.memory.AppendDailyLog("assistant", reply)
	}
}

func runTurn(ctx context.Context, runner *blades.Runner, sessMgr *session.Manager, sessionID, text string, opts TurnOptions) (string, error) {
	if runner == nil {
		return "", fmt.Errorf("no runner")
	}
	if sessMgr == nil {
		return "", fmt.Errorf("no session manager")
	}

	sess, err := sessMgr.Get(sessionID)
	if err != nil {
		return "", err
	}
	msg := blades.UserMessage(text)
	collector := newStreamCollector(opts.Writer)
	recorder := newTurnRecorder(opts)
	recorder.recordUser(sessionID, text)

	for m, err := range runner.RunStream(ctx, msg, blades.WithSession(sess)) {
		if err != nil {
			recorder.recordError(sessionID, err)
			return collector.reply(), err
		}
		collector.consume(m)
	}

	reply := collector.reply()
	recorder.recordAssistant(sessionID, text, reply)
	if err := ensureTurnHistory(ctx, sess, text, reply); err != nil {
		return reply, err
	}
	if err := sessMgr.Save(sess); err != nil {
		return reply, err
	}
	return reply, nil
}

func ensureTurnHistory(ctx context.Context, sess blades.Session, userText, reply string) error {
	if sess == nil {
		return nil
	}
	history, err := sess.History(ctx)
	if err != nil {
		return err
	}
	if hasTrailingTurn(history, userText, reply) {
		return nil
	}
	if !hasTrailingMessage(history, blades.RoleUser, userText) {
		userMessage := blades.UserMessage(userText)
		if err := sess.Append(ctx, userMessage); err != nil {
			return err
		}
		history = append(history, userMessage)
	}
	if strings.TrimSpace(reply) == "" || hasTrailingMessage(history, blades.RoleAssistant, reply) {
		return nil
	}
	return sess.Append(ctx, blades.AssistantMessage(reply))
}

func hasTrailingMessage(history []*blades.Message, role blades.Role, text string) bool {
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	return last != nil && last.Role == role && last.Text() == text
}

func hasTrailingTurn(history []*blades.Message, userText, reply string) bool {
	if len(history) < 2 {
		return false
	}
	prev := history[len(history)-2]
	last := history[len(history)-1]
	if prev == nil || last == nil {
		return false
	}
	if prev.Role != blades.RoleUser || prev.Text() != userText {
		return false
	}
	if strings.TrimSpace(reply) == "" {
		return last.Role == blades.RoleUser && last.Text() == userText
	}
	return last.Role == blades.RoleAssistant && last.Text() == reply
}
