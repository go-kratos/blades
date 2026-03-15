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
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

const channelName = "cli"

// Channel is a bubbletea-based interactive terminal channel.
type Channel struct {
	sessionID   string
	reload      func() error
	stop        func() error
	debug       bool
	noAltScreen bool
	cmdProc     *command.Processor
}

// Option configures a Channel.
type Option func(*Channel)

// WithReload sets the function called when the user issues /reload.
func WithReload(fn func() error) Option {
	return func(c *Channel) { c.reload = fn }
}

// WithStop sets the function called when the user issues /stop.
func WithStop(fn func() error) Option {
	return func(c *Channel) { c.stop = fn }
}

// WithDebug enables verbose error output.
func WithDebug(enabled bool) Option {
	return func(c *Channel) { c.debug = enabled }
}

// WithNoAltScreen switches to a plain line-based I/O loop instead of the
// bubbletea TUI. In this mode stdin is read via the terminal's cooked-mode
// line editor (bufio.Scanner), so the OS/Windows IME controls the input field
// and the pre-edit composition window is positioned correctly. Output is
// printed directly to stdout, enabling native terminal text selection.
func WithNoAltScreen() Option {
	return func(c *Channel) { c.noAltScreen = true }
}

// New creates a CLI Channel for the given session ID.
func New(sessionID string, opts ...Option) *Channel {
	c := &Channel{
		sessionID: sessionID,
		cmdProc:   command.NewProcessor(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name implements channel.Channel.
func (c *Channel) Name() string { return channelName }

// detectGlamourStyle queries the terminal background colour BEFORE bubbletea
// takes over stdin/stdout, so the OSC response never leaks into the TUI.
// Returns the glamour style name and whether the background is dark.
func detectGlamourStyle() (style string, isDark bool) {
	isDark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	if isDark {
		return "dark", true
	}
	return "light", false
}

// Start implements channel.Channel. It runs the bubbletea TUI (or the simple
// line-based loop when WithNoAltScreen is set) and blocks until the user quits
// or ctx is cancelled.
func (c *Channel) Start(ctx context.Context, handler channel.StreamHandler) error {
	if c.noAltScreen {
		return c.startSimple(ctx, handler)
	}
	// Detect dark/light BEFORE p.Run() so the OSC 11 query doesn't interfere
	// with bubbletea's input handling.
	glamourStyle, isDark := detectGlamourStyle()
	m := newModel(ctx, handler, c.sessionID, c.reload, c.stop, c.debug, glamourStyle, isDark, c.cmdProc)
	// AltScreen is declared in View() — no program options needed.
	// Real cursor is used (SetVirtualCursor(false) in newModel) so that
	// ConPTY/IME can track the physical cursor and position the pre-edit window.
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// startSimple is a line-based fallback that reads input from the terminal's
// cooked-mode line editor (bufio.Scanner) and writes responses directly to
// stdout. Because bubbletea never takes ownership of stdin, the OS/Windows IME
// can position the pre-edit composition window correctly.
func (c *Channel) startSimple(ctx context.Context, handler channel.StreamHandler) error {
	fmt.Println("⚡ blades  (simple mode — /help for commands, Ctrl+C to quit)")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		fmt.Print("❯ ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			env := &command.Environment{
				SessionID:         c.sessionID,
				ReloadFunc:        c.reload,
				StopFunc:          c.stop,
				SwitchSessionFunc: func(sessionID string) error { c.sessionID = sessionID; return nil },
				ClearFunc:         func() error { return nil }, // Not applicable in simple mode
				Processor:         c.cmdProc,
			}

			result, err := c.cmdProc.Process(ctx, line, env)
			if err != nil {
				fmt.Fprintln(os.Stderr, "command error:", err)
				continue
			}

			if result != nil {
				if result.IsError {
					fmt.Fprintln(os.Stderr, result.Message)
				} else {
					fmt.Println(result.Message)
				}
				if result.ShouldQuit {
					return nil
				}
			}
			continue
		}

		fmt.Println()
		w := &simpleWriter{}
		_, err := handler(ctx, c.sessionID, line, w)
		fmt.Println()
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
	}
}

// simpleWriter prints streaming output directly to stdout without any TUI.
type simpleWriter struct{}

func (w *simpleWriter) WriteText(chunk string) { fmt.Print(chunk) }

func (w *simpleWriter) WriteEvent(e channel.Event) {
	switch e.Kind {
	case channel.EventToolStart:
		fmt.Printf("🔧 %s …\n", e.Name)
	case channel.EventToolEnd:
		fmt.Println()
	}
}
