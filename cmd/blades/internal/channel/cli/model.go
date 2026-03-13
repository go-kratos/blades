package cli

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
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
	ctx          context.Context
	cancel       context.CancelFunc
	handler      channel.StreamHandler
	sessionID    string
	reload       func() error
	debug        bool
	glamourStyle string // "dark" or "light"
	isDark       bool

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
	isDark bool,
) *model {
	ctx, cancel := context.WithCancel(ctx)
	st := newStyles(isDark)
	ld := lipgloss.LightDark(isDark)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#7C3AED"), lipgloss.Color("#A78BFA")))

	ti := textinput.New()
	ti.Placeholder = "Type a message — /help for commands"
	ti.CharLimit = 8000
	ti.Prompt = "❯ "
	// Style the prompt with the success colour.
	s := textinput.DefaultStyles(isDark)
	s.Focused.Prompt = lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("#059669"), lipgloss.Color("#34D399")))
	s.Cursor.Shape = tea.CursorBar
	ti.SetStyles(s)
	// Use a real cursor so ConPTY/IME can track its position.
	ti.SetVirtualCursor(false)
	_ = ti.Focus()

	return &model{
		ctx:          ctx,
		cancel:       cancel,
		handler:      handler,
		sessionID:    sessionID,
		reload:       reload,
		debug:        debug,
		glamourStyle: glamourStyle,
		isDark:       isDark,
		styles:       st,
		state:        stateInput,
		spinner:      sp,
		input:        ti,
	}
}
