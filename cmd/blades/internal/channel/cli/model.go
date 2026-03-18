package cli

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

type appState int

const (
	stateInput   appState = iota
	stateRunning          // handler goroutine is running
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

// model is the bubbletea application model.
type model struct {
	// Injected
	ctx           context.Context
	cancel        context.CancelFunc
	handler       channel.StreamHandler
	sessionID     string
	stop          func() error
	switchSession func(string) error
	clearSession  func(string) error
	debug         bool
	glamourStyle  string // "dark" or "light"
	isDark        bool
	cmdProc       *command.Processor

	// Styles (built once from isDark)
	styles appStyles

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
	cancelTurn  context.CancelFunc // cancels the current turn's context when /stop or Escape
	stopCh      chan struct{}      // sent on when user stops; unblocks wait so we can finish turn without waiting for handler

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
	stop func() error,
	debug bool,
	glamourStyle string,
	isDark bool,
	cmdProc *command.Processor,
) *model {
	ctx, cancel := context.WithCancel(ctx)
	st := newStyles(isDark)
	ld := lipgloss.LightDark(isDark)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF")))

	ti := textinput.New()
	ti.Placeholder = "Message or /help"
	ti.CharLimit = 8000
	ti.Prompt = "> "
	s := textinput.DefaultStyles(isDark)
	s.Focused.Prompt = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#1D4ED8"), lipgloss.Color("#93C5FD")))
	s.Focused.Text = lipgloss.NewStyle().Bold(true)
	s.Blurred.Text = lipgloss.NewStyle().Bold(true)
	s.Focused.Placeholder = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF")))
	s.Blurred.Placeholder = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF")))
	s.Cursor.Shape = tea.CursorBar
	ti.SetStyles(s)
	// Use a real cursor so ConPTY/IME can track its position.
	ti.SetVirtualCursor(false)
	_ = ti.Focus()

	mod := &model{
		ctx:          ctx,
		cancel:       cancel,
		handler:      handler,
		sessionID:    sessionID,
		stop:         stop,
		debug:        debug,
		glamourStyle: glamourStyle,
		isDark:       isDark,
		styles:       st,
		state:        stateInput,
		spinner:      sp,
		input:        ti,
		cmdProc:      cmdProc,
		stopCh:       make(chan struct{}, 1),
	}
	if stop == nil {
		mod.stop = func() error {
			if mod.cancelTurn != nil {
				mod.cancelTurn()
				mod.cancelTurn = nil
			}
			return nil
		}
	}
	return mod
}
