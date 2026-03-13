// Package cli implements a full-screen interactive terminal channel using
// bubbletea for the event loop, lipgloss for styling, glamour for markdown
// rendering, and bubbles components for input and viewport.
//
// Layout (altscreen):
//
// ┌──────────────────────────────────────────────────────┐
// │  ⚡ blades  session: xyz   (banner, inside viewport)  │
// │  ──────────────────────────────────────────────────  │
// │  You: hello                                          │
// │  ╭─ [1] 🔧 search_web ─────────────────────────────╮ │
// │  │ ▶ result preview  (press 1 to expand)           │ │
// │  ╰──────────────────────────────────────────────────╯ │
// │  Assistant:                                          │
// │  Here is the answer…                                 │
// ├──────────────────────────────────────────────────────┤
// │ ⠸ thinking…  /  PgUp·PgDn to scroll                 │ ← status bar
// ├──────────────────────────────────────────────────────┤
// │ ❯ Type a message…                                    │ ← input
// └──────────────────────────────────────────────────────┘
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
)

// ── Colours ───────────────────────────────────────────────────────────────────

var (
	clrPrimary = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}
	clrSuccess = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"}
	clrWarning = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FCD34D"}
	clrError   = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"}
	clrUser    = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"}
	clrDim     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	bannerStyle      = lipgloss.NewStyle().Bold(true).Foreground(clrPrimary)
	dimStyle         = lipgloss.NewStyle().Foreground(clrDim)
	hintStyle        = lipgloss.NewStyle().Foreground(clrDim).Italic(true)
	errStyle         = lipgloss.NewStyle().Bold(true).Foreground(clrError)
	userLabelStyle   = lipgloss.NewStyle().Bold(true).Foreground(clrUser)
	assistLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(clrSuccess)
	toolHeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(clrWarning)

	toolBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrDim).
			PaddingLeft(1).PaddingRight(1)

	toolBoxActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrWarning).
				PaddingLeft(1).PaddingRight(1)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrDim)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(clrDim).PaddingLeft(1)
)

// ── Internal stream message ───────────────────────────────────────────────────

type streamMsg struct {
	text  string
	event *channel.Event
	done  bool
	err   error
}

// chanWriter implements channel.Writer using a buffered channel.
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

// ── Domain types ──────────────────────────────────────────────────────────────

type appState int

const (
	stateInput   appState = iota
	stateRunning          // handler goroutine running
)

// toolSection tracks one tool invocation within a turn.
type toolSection struct {
	idx      int    // 1-based display number within the turn
	id       string // call ID for Start→End correlation
	name     string
	input    string
	output   string
	complete bool
	expanded bool
}

// convTurn is a completed or in-progress conversation exchange.
type convTurn struct {
	user      string
	assistant string // raw markdown from the model
	tools     []toolSection
	rendered  string // glamour-rendered (set when turn completes)
	isMeta    bool   // system/command message
	metaText  string // pre-rendered content for meta turns
}

// ── bubbletea Model ──────────────────────────────────────────────────────────

type model struct {
	// Injected
	ctx          context.Context
	cancel       context.CancelFunc
	handler      channel.StreamHandler
	sessionID    string
	reload       func() error
	debug        bool
	glamourStyle string // "dark" or "light", detected before bubbletea starts

	// UI components
	state   appState
	spinner spinner.Model
	input   textinput.Model
	vp      viewport.Model
	vpReady bool

	// Conversation
	turns       []*convTurn
	pastContent string // pre-built content of finished turns

	// Current streaming turn
	streamCh    <-chan streamMsg
	streamBuf   strings.Builder
	streamTools []toolSection
	toolCounter int

	// Glamour renderer (recreated on resize, uses fixed glamourStyle)
	glamour *glamour.TermRenderer

	// Terminal size
	width  int
	height int

	// Misc
	err      error
	quitting bool
}

func newModel(
	ctx context.Context,
	handler channel.StreamHandler,
	sessionID string,
	reload func() error,
	debug bool,
	glamourStyle string,
) *model {
	ctx, cancel := context.WithCancel(ctx)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(clrPrimary)

	ti := textinput.New()
	ti.Placeholder = "Type a message — /help for commands"
	ti.CharLimit = 8000
	ti.Prompt = lipgloss.NewStyle().Bold(true).Foreground(clrSuccess).Render("❯ ")
	ti.Focus()

	return &model{
		ctx:          ctx,
		cancel:       cancel,
		handler:      handler,
		sessionID:    sessionID,
		reload:       reload,
		debug:        debug,
		glamourStyle: glamourStyle,
		state:        stateInput,
		spinner:      sp,
		input:        ti,
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Window resize ──────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpH := m.vpHeight()

		if !m.vpReady {
			m.vp = viewport.New(m.width, vpH)
			m.vpReady = true
		} else {
			m.vp.Width = m.width
			m.vp.Height = vpH
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
		m.input.Width = m.width - 6 // border(2) + prompt(~3) + margin
		m.rebuildPastContent()
		m.refreshViewport(true)

	// ── Spinner tick ───────────────────────────────────────────────────────
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.state == stateRunning {
			m.refreshViewport(false) // repaint spinner frame
		}

	// ── Stream events ──────────────────────────────────────────────────────
	case streamMsg:
		return m.handleStream(msg)

	// ── Keyboard ───────────────────────────────────────────────────────────
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Update viewport (mouse, scroll keys)
	var vpCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	cmds = append(cmds, vpCmd)

	// Update textinput when waiting for input
	if m.state == stateInput {
		var tiCmd tea.Cmd
		m.input, tiCmd = m.input.Update(msg)
		cmds = append(cmds, tiCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global shortcuts
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyCtrlD:
		if m.input.Value() == "" {
			return m.quit()
		}
	case tea.KeyPgUp:
		m.vp.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.vp.HalfViewDown()
		return m, nil
	}

	// During streaming only global shortcuts are active
	if m.state == stateRunning {
		var spCmd tea.Cmd
		m.spinner, spCmd = m.spinner.Update(msg)
		return m, spCmd
	}

	// ── stateInput ─────────────────────────────────────────────────────────
	switch msg.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			return m, nil
		}
		m.input.SetValue("")
		if strings.HasPrefix(val, "/") {
			return m.handleSlash(val)
		}
		return m.startTurn(val)

	case tea.KeyRunes:
		// Number keys 1–9 toggle tool sections when the input field is empty
		if m.input.Value() == "" && len(msg.Runes) == 1 {
			r := msg.Runes[0]
			if r >= '1' && r <= '9' {
				if m.toggleLastTool(int(r-'0') - 1) {
					m.rebuildPastContent()
					m.refreshViewport(false)
					return m, nil
				}
			}
		}
	}

	// Forward remaining keys to textinput + viewport
	var tiCmd, vpCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	m.vp, vpCmd = m.vp.Update(msg)
	return m, tea.Batch(tiCmd, vpCmd)
}

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
	return m, waitStream(m.streamCh)
}

func (m *model) finishTurn(turnErr error) (tea.Model, tea.Cmd) {
	m.state = stateInput
	m.input.Focus()
	m.streamCh = nil
	m.err = turnErr

	// Fill in the placeholder turn appended by startTurn
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
	return m, textinput.Blink
}

func (m *model) handleSlash(line string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/exit", "/quit":
		return m.quit()

	case "/help":
		md := "## Commands\n\n" +
			"| Command | Description |\n" +
			"|---------|-------------|\n" +
			"| `/help` | Show this help |\n" +
			"| `/reload` | Hot-reload skills |\n" +
			"| `/session [id]` | Show or switch session |\n" +
			"| `/clear` | Clear conversation |\n" +
			"| `/exit` | Quit |\n\n" +
			"## Keyboard shortcuts\n\n" +
			"- **1–9** — toggle tool call details in last response\n" +
			"- **PgUp / PgDn** — scroll conversation\n" +
			"- **Ctrl+C** — quit\n"
		m.addMeta(md)

	case "/reload":
		if m.reload == nil {
			m.addMetaRaw(dimStyle.Render("(reload not configured)"))
			break
		}
		if err := m.reload(); err != nil {
			m.addMetaRaw(errStyle.Render("reload failed: " + err.Error()))
		} else {
			m.addMetaRaw(dimStyle.Render("✓ skills reloaded"))
		}

	case "/session":
		if len(parts) < 2 {
			m.addMetaRaw(dimStyle.Render("current session: " + m.sessionID))
		} else {
			m.sessionID = parts[1]
			m.addMetaRaw(dimStyle.Render("switched to session: " + m.sessionID))
		}

	case "/clear":
		m.turns = nil
		m.pastContent = ""
		m.err = nil

	default:
		m.addMetaRaw(hintStyle.Render(fmt.Sprintf("unknown command %q — type /help", parts[0])))
	}

	m.rebuildPastContent()
	m.refreshViewport(true)
	return m, nil
}

func (m *model) startTurn(text string) (tea.Model, tea.Cmd) {
	m.state = stateRunning
	m.input.Blur()
	m.streamBuf.Reset()
	m.streamTools = nil
	m.toolCounter = 0
	m.err = nil

	// Placeholder turn (completed by finishTurn)
	m.turns = append(m.turns, &convTurn{user: text})
	m.rebuildPastContent()
	m.refreshViewport(true)

	ch := make(chan streamMsg, 512)
	m.streamCh = ch

	go func() {
		w := &chanWriter{ctx: m.ctx, ch: ch}
		_, err := m.handler(m.ctx, m.sessionID, text, w)
		ch <- streamMsg{done: true, err: err}
	}()

	return m, tea.Batch(m.spinner.Tick, waitStream(ch))
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	m.quitting = true
	return m, tea.Quit
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m *model) View() string {
	if m.quitting {
		return dimStyle.Render("Bye! 👋") + "\n"
	}
	if !m.vpReady {
		return ""
	}
	return m.vp.View() + "\n" +
		m.statusBar() + "\n" +
		inputBorderStyle.Width(m.width-2).Render(m.input.View())
}

func (m *model) statusBar() string {
	if m.state == stateRunning {
		return statusBarStyle.Render(m.spinner.View() + " thinking…")
	}
	if m.err != nil {
		return statusBarStyle.Render(errStyle.Render("⚠  " + m.err.Error()))
	}
	for i := len(m.turns) - 1; i >= 0; i-- {
		if !m.turns[i].isMeta {
			if len(m.turns[i].tools) > 0 {
				return statusBarStyle.Render(hintStyle.Render("1–9 toggle tools · PgUp/PgDn scroll · /help commands"))
			}
			break
		}
	}
	return statusBarStyle.Render(hintStyle.Render("PgUp/PgDn to scroll · /help for commands"))
}

// ── Content construction ──────────────────────────────────────────────────────

// vpHeight returns the height available for the viewport.
// Footer = 1 status line + 3 input-border lines + 1 separator = 5.
func (m *model) vpHeight() int {
	h := m.height - 5
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) refreshViewport(scrollToBottom bool) {
	if !m.vpReady {
		return
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(m.buildContent())
	if scrollToBottom || atBottom {
		m.vp.GotoBottom()
	}
}

// rebuildPastContent re-renders all completed turns into a cached string.
// Call this whenever turns are added/modified, but NOT on every streaming token.
func (m *model) rebuildPastContent() {
	var b strings.Builder

	// Banner header
	b.WriteString(bannerStyle.Render("⚡ blades"))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("session: " + m.sessionID))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("Type your message · /help · Ctrl+C to quit"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	for _, t := range m.turns {
		b.WriteString(m.renderTurn(t))
		b.WriteString("\n")
	}
	m.pastContent = b.String()
}

// buildContent assembles the full viewport content: past turns + streaming section.
func (m *model) buildContent() string {
	if m.state == stateRunning {
		return m.pastContent + m.renderStreaming()
	}
	return m.pastContent
}

func (m *model) renderTurn(t *convTurn) string {
	if t.isMeta {
		return t.metaText
	}
	var b strings.Builder

	b.WriteString(userLabelStyle.Render("You:"))
	b.WriteString(" ")
	b.WriteString(t.user)
	b.WriteString("\n")

	for i := range t.tools {
		b.WriteString(m.renderToolSection(&t.tools[i]))
		b.WriteString("\n")
	}

	if t.rendered != "" {
		b.WriteString(assistLabelStyle.Render("Assistant:"))
		b.WriteString("\n")
		b.WriteString(t.rendered)
	}

	return b.String()
}

func (m *model) renderStreaming() string {
	var b strings.Builder
	for i := range m.streamTools {
		b.WriteString(m.renderToolSection(&m.streamTools[i]))
		b.WriteString("\n")
	}
	if m.streamBuf.Len() > 0 {
		b.WriteString(assistLabelStyle.Render("Assistant:"))
		b.WriteString("\n")
		b.WriteString(m.streamBuf.String())
	}
	return b.String()
}

func (m *model) renderToolSection(ts *toolSection) string {
	maxW := m.width - 6
	if maxW < 20 {
		maxW = 20
	}

	label := fmt.Sprintf("[%d] 🔧 %s", ts.idx, ts.name)

	if !ts.complete {
		content := toolHeaderStyle.Render("⠸ "+label) + "  " + dimStyle.Render("running…")
		if ts.input != "" {
			content += "\n" + dimStyle.Render("→ ") + truncate(ts.input, maxW-4)
		}
		return toolBoxActiveStyle.Width(maxW).Render(content)
	}

	if !ts.expanded {
		preview := ""
		if ts.output != "" {
			preview = "  " + dimStyle.Render(truncate(singleLine(ts.output), maxW-len(label)-10))
		}
		return toolBoxStyle.Width(maxW).Render("▶ " + toolHeaderStyle.Render(label) + preview)
	}

	var content strings.Builder
	content.WriteString("▼ " + toolHeaderStyle.Render(label))
	if ts.input != "" {
		content.WriteString("\n" + dimStyle.Render("Input:  ") + ts.input)
	}
	if ts.output != "" {
		content.WriteString("\n" + dimStyle.Render("Output: ") + ts.output)
	}
	return toolBoxStyle.Width(maxW).Render(content.String())
}

// toggleLastTool toggles the tool at 0-based toolIdx in the most recent real turn.
func (m *model) toggleLastTool(toolIdx int) bool {
	for i := len(m.turns) - 1; i >= 0; i-- {
		if !m.turns[i].isMeta {
			if toolIdx < len(m.turns[i].tools) {
				m.turns[i].tools[toolIdx].expanded = !m.turns[i].tools[toolIdx].expanded
				return true
			}
			return false
		}
	}
	return false
}

func (m *model) addMeta(md string) {
	t := &convTurn{isMeta: true}
	if m.glamour != nil {
		if rendered, err := m.glamour.Render(md); err == nil {
			t.metaText = rendered
		} else {
			t.metaText = md
		}
	} else {
		t.metaText = md
	}
	m.turns = append(m.turns, t)
}

func (m *model) addMetaRaw(s string) {
	m.turns = append(m.turns, &convTurn{isMeta: true, metaText: s})
}

// ── Utility ───────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if max <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func singleLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

// ── Channel (public API) ──────────────────────────────────────────────────────

const channelName = "cli"

// Channel is a bubbletea-based interactive terminal channel.
type Channel struct {
	sessionID string
	reload    func() error
	debug     bool
}

// Option configures a Channel.
type Option func(*Channel)

// WithReload sets the function called when the user issues /reload.
func WithReload(fn func() error) Option {
	return func(c *Channel) { c.reload = fn }
}

// WithPrompt is kept for API compatibility but has no effect on the TUI prompt.
func WithPrompt(_ string) Option { return func(*Channel) {} }

// WithDebug enables verbose error output.
func WithDebug(enabled bool) Option {
	return func(c *Channel) { c.debug = enabled }
}

// New creates a CLI Channel for the given session ID.
func New(sessionID string, opts ...Option) *Channel {
	c := &Channel{sessionID: sessionID}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name implements channel.Channel.
func (c *Channel) Name() string { return channelName }

// detectGlamourStyle queries the terminal background color BEFORE bubbletea
// takes over stdin/stdout, so the OSC response never leaks into the TUI.
func detectGlamourStyle() string {
	if lipgloss.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

// Start implements channel.Channel. It runs the bubbletea TUI and blocks until
// the user quits or ctx is cancelled.
func (c *Channel) Start(ctx context.Context, handler channel.StreamHandler) error {
	// Detect dark/light BEFORE p.Run() so the OSC 11 query doesn't interfere
	// with bubbletea's input handling.
	style := detectGlamourStyle()
	m := newModel(ctx, handler, c.sessionID, c.reload, c.debug, style)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
