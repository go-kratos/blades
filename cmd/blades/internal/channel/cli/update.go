package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

// ── Stream bridge ─────────────────────────────────────────────────────────────

// streamMsg carries a single item from the handler goroutine to the bubbletea
// event loop.
type streamMsg struct {
	text  string
	event *channel.Event
	done  bool
	err   error
}

// chanWriter implements channel.Writer by forwarding writes to a buffered
// channel that the bubbletea loop drains via waitStream.
type chanWriter struct {
	ctx context.Context
	ch  chan<- streamMsg
}

func (w *chanWriter) WriteText(chunk string) {
	select {
	case w.ch <- streamMsg{text: chunk}:
	case <-w.ctx.Done():
	}
}

func (w *chanWriter) WriteEvent(e channel.Event) {
	select {
	case w.ch <- streamMsg{event: &e}:
	case <-w.ctx.Done():
	}
}

// waitStream returns a tea.Cmd that blocks until the next item on ch arrives.
func waitStream(ch <-chan streamMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// waitStreamOrStop blocks until the next stream message or a stop signal (e.g. user pressed Esc).
// When stopCh is sent, returns a synthetic done message so we finish the turn without waiting for the handler.
func waitStreamOrStop(ch <-chan streamMsg, stopCh <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-ch:
			return m
		case <-stopCh:
			return streamMsg{done: true, err: context.Canceled}
		}
	}
}

// cliHelpSuffix is appended to /help output only in the CLI channel.
const cliHelpSuffix = `## Keyboard Shortcuts

- **1–9** — toggle tool call details
- **PgUp / PgDn** — scroll conversation
- **Esc** — stop current response (agent stops; you can keep chatting)
- **Ctrl+C** — quit
`

// ── Init ─────────────────────────────────────────────────────────────────────

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpH := m.vpHeight()

		if !m.vpReady {
			m.vp = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(vpH))
			m.vpReady = true
		} else {
			m.vp.SetWidth(m.width)
			m.vp.SetHeight(vpH)
		}
		// Recreate glamour with the correct wrap width.
		// Use the pre-detected style (never WithAutoStyle inside bubbletea —
		// that sends an OSC 11 query whose response leaks into the display).
		if gr, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(m.glamourStyle),
			glamour.WithWordWrap(m.width-6),
		); err == nil {
			m.glamour = gr
		}
		m.input.SetWidth(m.width - 6) // border(2) + prompt(~3) + margin
		m.rebuildPastContent()
		m.refreshViewport(true)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.state == stateRunning {
			m.refreshViewport(false)
		}

	case streamMsg:
		return m.handleStream(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	cmds = append(cmds, vpCmd)

	if m.state == stateInput {
		var tiCmd tea.Cmd
		m.input, tiCmd = m.input.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	return m, tea.Batch(cmds...)
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Global shortcuts — active in all states.
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "ctrl+d":
		if m.input.Value() == "" {
			return m.quit()
		}
	case "pgup":
		m.vp.HalfPageUp()
		return m, nil
	case "pgdown":
		m.vp.HalfPageDown()
		return m, nil
	}

	// Number keys 1–9 toggle tool sections in both stateInput and stateRunning.
	if len(msg.Text) == 1 {
		r := rune(msg.Text[0])
		if r >= '1' && r <= '9' {
			idx := int(r-'0') - 1
			if m.state == stateRunning {
				if idx < len(m.streamTools) {
					m.streamTools[idx].expanded = !m.streamTools[idx].expanded
					m.refreshViewport(false)
				}
				return m, nil
			}
			if m.input.Value() == "" {
				if m.toggleLastTool(idx) {
					m.rebuildPastContent()
					m.refreshViewport(false)
					return m, nil
				}
			}
		}
	}

	// During streaming: Escape stops the response; other keys only tick spinner.
	if m.state == stateRunning {
		if msg.String() == "esc" && m.stop != nil {
			_ = m.stop()
			// Unblock the stream wait so we finish turn immediately instead of waiting for the handler (which may never return if the backend ignores context).
			select {
			case m.stopCh <- struct{}{}:
			default:
			}
			return m, waitStreamOrStop(m.streamCh, m.stopCh)
		}
		var spCmd tea.Cmd
		m.spinner, spCmd = m.spinner.Update(msg)
		return m, spCmd
	}

	// stateInput: handle Enter and forward everything else to textinput.
	if msg.Code == tea.KeyEnter {
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			return m, nil
		}
		m.input.SetValue("")
		if strings.HasPrefix(val, "/") {
			return m.handleSlash(val)
		}
		return m.startTurn(val)
	}

	var tiCmd, vpCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	m.vp, vpCmd = m.vp.Update(msg)
	return m, tea.Batch(tiCmd, vpCmd)
}

// ── Slash commands ────────────────────────────────────────────────────────────

func (m *model) handleSlash(line string) (tea.Model, tea.Cmd) {
	env := &command.Environment{
		SessionID:  m.sessionID,
		ReloadFunc: m.reload,
		StopFunc:   m.stop,
		SwitchSessionFunc: func(sessionID string) error {
			m.sessionID = sessionID
			return nil
		},
		ClearFunc: func() error {
			m.turns = nil
			m.pastContent = ""
			m.err = nil
			return nil
		},
		Processor: m.cmdProc,
		Custom: map[string]interface{}{
			"helpSuffix": cliHelpSuffix,
		},
	}

	result, err := m.cmdProc.Process(m.ctx, line, env)
	if err != nil {
		m.addMetaRaw(m.styles.err.Render("Command error: " + err.Error()))
	} else if result != nil {
		if result.IsError {
			m.addMetaRaw(m.styles.err.Render(result.Message))
		} else {
			m.addMeta(result.Message)
		}
		if result.ShouldQuit {
			return m.quit()
		}
	}

	m.rebuildPastContent()
	m.refreshViewport(true)
	return m, nil
}

// ── Stream handling ───────────────────────────────────────────────────────────

func (m *model) handleStream(msg streamMsg) (tea.Model, tea.Cmd) {
	if msg.done {
		return m.finishTurn(msg.err)
	}

	if msg.event != nil {
		e := msg.event
		switch e.Kind {
		case channel.EventToolStart:
			m.toolCounter++
			m.streamTools = append(m.streamTools, toolSection{
				idx:      m.toolCounter,
				id:       e.ID,
				name:     e.Name,
				input:    e.Input,
				expanded: true,
			})
		case channel.EventToolEnd:
			for i := len(m.streamTools) - 1; i >= 0; i-- {
				if !m.streamTools[i].complete && (m.streamTools[i].id == e.ID || m.streamTools[i].name == e.Name) {
					m.streamTools[i].output = e.Output
					m.streamTools[i].complete = true
					m.streamTools[i].expanded = false
					break
				}
			}
		}
	} else {
		m.streamBuf.WriteString(msg.text)
	}

	m.refreshViewport(false)
	return m, waitStreamOrStop(m.streamCh, m.stopCh)
}

func (m *model) finishTurn(turnErr error) (tea.Model, tea.Cmd) {
	m.state = stateInput
	focusCmd := m.input.Focus()
	m.streamCh = nil
	if m.cancelTurn != nil {
		m.cancelTurn()
		m.cancelTurn = nil
	}
	if turnErr != nil && errors.Is(turnErr, context.Canceled) {
		m.addMeta("⏹ Response stopped. You can continue the conversation.")
		turnErr = nil
	}
	m.err = turnErr

	if len(m.turns) > 0 {
		t := m.turns[len(m.turns)-1]
		t.tools = append([]toolSection(nil), m.streamTools...)
		t.assistant = m.streamBuf.String()
		if t.assistant != "" && m.glamour != nil {
			if rendered, err := m.glamour.Render(t.assistant); err == nil {
				t.rendered = rendered
			} else {
				t.rendered = t.assistant
			}
		} else {
			t.rendered = t.assistant
		}
	}

	m.streamBuf.Reset()
	m.streamTools = nil
	m.toolCounter = 0

	m.rebuildPastContent()
	m.refreshViewport(true)
	return m, tea.Batch(focusCmd, textinput.Blink)
}

func (m *model) startTurn(text string) (tea.Model, tea.Cmd) {
	m.state = stateRunning
	m.input.Blur()
	m.streamBuf.Reset()
	m.streamTools = nil
	m.toolCounter = 0
	m.err = nil

	m.turns = append(m.turns, &convTurn{user: text})
	m.rebuildPastContent()
	m.refreshViewport(true)

	ch := make(chan streamMsg, 512)
	m.streamCh = ch

	turnCtx, turnCancel := context.WithCancel(m.ctx)
	m.cancelTurn = turnCancel

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- streamMsg{done: true, err: fmt.Errorf("handler panic: %v", r)}
			}
		}()
		w := &chanWriter{ctx: turnCtx, ch: ch}
		_, err := m.handler(turnCtx, m.sessionID, text, w)
		ch <- streamMsg{done: true, err: err}
	}()

	return m, tea.Batch(m.spinner.Tick, waitStreamOrStop(ch, m.stopCh))
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	m.quitting = true
	return m, tea.Quit
}
