package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

func captureStdIO(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
	})

	fn()

	_ = outW.Close()
	_ = errW.Close()
	outData, _ := io.ReadAll(outR)
	errData, _ := io.ReadAll(errR)
	return string(outData), string(errData)
}

func TestChannelOptionsAndSimpleMode(t *testing.T) {
	var switched, cleared bool
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if _, err := w.WriteString("/session new-id\n/clear\nhello\n"); err != nil {
		t.Fatalf("stdin write: %v", err)
	}
	_ = w.Close()

	var gotSession, gotText string
	out, errOut := captureStdIO(t, func() {
		ch := New("session-1",
			WithStop(func() error { return nil }),
			WithDebug(true),
			WithNoAltScreen(),
			WithSwitchSession(func(string) error { switched = true; return nil }),
			WithClearSession(func(string) error { cleared = true; return nil }),
			WithIO(r, os.Stdout, os.Stderr),
		)
		if ch.Name() != "cli" || !ch.debug || !ch.noAltScreen {
			t.Fatalf("channel = %+v", ch)
		}

		err := ch.Start(context.Background(), func(ctx context.Context, sid, text string, w channel.Writer) (string, error) {
			gotSession, gotText = sid, text
			w.WriteText("reply")
			w.WriteEvent(channel.Event{Kind: channel.EventToolStart, Name: "exec"})
			w.WriteEvent(channel.Event{Kind: channel.EventToolEnd, Name: "exec"})
			return "reply", nil
		})
		if err != nil {
			t.Fatalf("Start simple mode: %v", err)
		}
	})
	if !switched || !cleared {
		t.Fatalf("switch/clear callbacks not invoked: switched=%v cleared=%v", switched, cleared)
	}
	if gotSession != "new-id" || gotText != "hello" {
		t.Fatalf("handler got session=%q text=%q", gotSession, gotText)
	}
	if !strings.Contains(out, "simple mode") || !strings.Contains(out, "reply") {
		t.Fatalf("stdout = %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestModelRenderingAndHelpers(t *testing.T) {
	mod := newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", nil, true, "dark", true, command.NewProcessor())

	if mod.stop == nil {
		t.Fatalf("newModel not initialized: %+v", mod)
	}

	mod.width = 80
	mod.height = 20
	mod.vp = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	mod.vpReady = true
	mod.turns = []*convTurn{{
		user:      "hello",
		assistant: "world",
		rendered:  "world",
		tools: []toolSection{{
			idx:      1,
			id:       "tool-1",
			name:     "exec",
			input:    "pwd",
			output:   "ok",
			complete: true,
		}},
	}}
	mod.rebuildPastContent()
	if !strings.Contains(mod.buildContent(), "User") {
		t.Fatalf("buildContent = %q", mod.buildContent())
	}
	if strings.Index(mod.buildContent(), "Assistant") > strings.Index(mod.buildContent(), "1. exec") {
		t.Fatalf("expected tools below Assistant, content = %q", mod.buildContent())
	}
	if !mod.toggleLastTool(0) || !mod.turns[0].tools[0].expanded {
		t.Fatalf("toggleLastTool failed: %+v", mod.turns[0].tools[0])
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := singleLine(" a\nb "); got != "a b" {
		t.Fatalf("singleLine = %q", got)
	}
	if got := mod.vpHeight(); got != 15 {
		t.Fatalf("vpHeight = %d", got)
	}
	if !strings.Contains(mod.statusBar(), "details") {
		t.Fatalf("statusBar = %q", mod.statusBar())
	}
	mod.state = stateRunning
	if !strings.Contains(mod.statusBar(), "thinking") {
		t.Fatalf("running statusBar = %q", mod.statusBar())
	}
	mod.state = stateInput
	mod.err = context.Canceled
	if !strings.Contains(mod.statusBar(), "canceled") {
		t.Fatalf("error statusBar = %q", mod.statusBar())
	}
	mod.err = nil
	mod.turns[0].tools[0].complete = false
	if !strings.Contains(mod.renderToolSection(&mod.turns[0].tools[0]), "running") {
		t.Fatalf("running tool section = %q", mod.renderToolSection(&mod.turns[0].tools[0]))
	}
	mod.turns[0].tools[0].complete = true
	mod.turns[0].tools[0].expanded = true
	if !strings.Contains(mod.renderToolSection(&mod.turns[0].tools[0]), "output:") {
		t.Fatalf("expanded tool section = %q", mod.renderToolSection(&mod.turns[0].tools[0]))
	}

	mod.addMetaRaw("meta")
	mod.addMeta("markdown")
	mod.resetConversationView()
	if len(mod.turns) != 0 || mod.pastContent != "" {
		t.Fatalf("resetConversationView failed: %+v", mod)
	}
}

func TestModelStreamLifecycle(t *testing.T) {
	mod := newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", nil, false, "dark", true, command.NewProcessor())
	mod.width = 80
	mod.height = 20
	mod.vp = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	mod.vpReady = true
	mod.rebuildPastContent()

	if _, cmd := mod.startTurn("hello"); cmd == nil {
		t.Fatal("expected startTurn to return command")
	}
	if mod.state != stateRunning || len(mod.turns) != 1 {
		t.Fatalf("startTurn state = %v turns=%d", mod.state, len(mod.turns))
	}

	if _, cmd := mod.handleStream(streamMsg{text: "chunk"}); cmd == nil || !strings.Contains(mod.streamBuf.String(), "chunk") {
		t.Fatalf("handleStream text failed: buf=%q", mod.streamBuf.String())
	}
	if _, cmd := mod.handleStream(streamMsg{event: &channel.Event{Kind: channel.EventToolStart, ID: "1", Name: "exec", Input: "pwd"}}); cmd == nil || len(mod.streamTools) != 1 {
		t.Fatalf("handleStream tool start failed: %+v", mod.streamTools)
	}
	if mod.streamTools[0].expanded {
		t.Fatalf("tool should start collapsed: %+v", mod.streamTools[0])
	}
	if _, cmd := mod.handleStream(streamMsg{event: &channel.Event{Kind: channel.EventToolEnd, ID: "1", Name: "exec", Output: "ok"}}); cmd == nil || !mod.streamTools[0].complete {
		t.Fatalf("handleStream tool end failed: %+v", mod.streamTools)
	}

	if _, cmd := mod.finishTurn(context.Canceled); cmd == nil {
		t.Fatal("expected finishTurn command")
	}
	if mod.state != stateInput || len(mod.turns) == 0 {
		t.Fatalf("finishTurn state = %v turns=%d", mod.state, len(mod.turns))
	}
	if !strings.Contains(mod.pastContent, "Response stopped") {
		t.Fatalf("finishTurn content = %q", mod.pastContent)
	}
}

func TestModelKeyAndSlashHandling(t *testing.T) {
	var switched string
	mod := newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", nil, false, "dark", true, command.NewProcessor())
	mod.width = 80
	mod.height = 20
	mod.vp = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	mod.vpReady = true
	mod.switchSession = func(s string) error { switched = s; return nil }

	mod.turns = []*convTurn{{tools: []toolSection{{idx: 1, name: "exec"}}}}
	if _, cmd := mod.handleKey(tea.KeyPressMsg{Text: "1"}); cmd != nil {
		t.Fatalf("expected nil cmd for tool toggle, got %v", cmd)
	}
	if !mod.turns[0].tools[0].expanded {
		t.Fatal("expected tool section to expand")
	}
	if _, cmd := mod.handleKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})); cmd == nil {
		t.Fatal("expected ctrl+c to return quit cmd")
	}

	mod.input.SetValue("/help")
	if _, cmd := mod.handleSlash("/session next"); cmd != nil && switched != "next" {
		t.Fatalf("expected session switch, cmd=%v switched=%q", cmd, switched)
	}

	mod = newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", func() error { return nil }, false, "dark", true, command.NewProcessor())
	mod.width = 80
	mod.height = 20
	mod.vp = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	mod.vpReady = true
	mod.state = stateRunning
	if _, cmd := mod.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})); cmd == nil {
		t.Fatal("expected esc during running to return wait cmd")
	}

	mod.state = stateInput
	mod.input.SetValue("hello")
	modelAfter, cmd := mod.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to start turn")
	}
	if modelAfter.(*model).state != stateRunning {
		t.Fatalf("expected running state, got %v", modelAfter.(*model).state)
	}

	if got := waitStream(make(chan streamMsg)); got == nil {
		t.Fatal("expected waitStream cmd")
	}
	if got := waitStreamOrStop(make(chan streamMsg), make(chan struct{})); got == nil {
		t.Fatal("expected waitStreamOrStop cmd")
	}
}

func TestCLIUpdateViewAndWriterHelpers(t *testing.T) {
	mod := newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", nil, false, "dark", true, command.NewProcessor())

	if cmd := mod.Init(); cmd == nil {
		t.Fatal("expected Init cmd")
	}

	modelAfter, cmd := mod.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mod = modelAfter.(*model)
	if !mod.vpReady || mod.width != 80 || mod.height != 24 {
		t.Fatalf("window size not applied: %+v", mod)
	}
	_ = cmd

	modelAfter, cmd = mod.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("expected spinner tick cmd")
	}
	mod = modelAfter.(*model)

	if _, cmd = mod.quit(); cmd == nil || !mod.quitting {
		t.Fatalf("quit failed: quitting=%v cmd=%v", mod.quitting, cmd)
	}
	if rendered := fmt.Sprint(mod.View()); !strings.Contains(rendered, "Bye!") {
		t.Fatalf("quitting view = %q", rendered)
	}

	ch := make(chan streamMsg, 2)
	w := &chanWriter{ctx: context.Background(), ch: ch}
	w.WriteText("chunk")
	w.WriteEvent(channel.Event{Kind: channel.EventToolStart, Name: "exec"})
	if got := waitStream(ch)().(streamMsg); got.text != "chunk" {
		t.Fatalf("waitStream got = %+v", got)
	}
	stopCh := make(chan struct{}, 1)
	stopCh <- struct{}{}
	if got := waitStreamOrStop(make(chan streamMsg), stopCh)().(streamMsg); !got.done || got.err != context.Canceled {
		t.Fatalf("waitStreamOrStop got = %+v", got)
	}

	if style, _ := detectGlamourStyle(); style != "dark" && style != "light" {
		t.Fatalf("detectGlamourStyle = %q", style)
	}
}

func TestHandleSlashErrorAndQuitBranches(t *testing.T) {
	mod := newModel(context.Background(), func(context.Context, string, string, channel.Writer) (string, error) {
		return "ok", nil
	}, "session-1", nil, false, "dark", true, command.NewProcessor())
	mod.width = 80
	mod.height = 20
	mod.vp = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	mod.vpReady = true
	mod.clearSession = func(string) error { return context.DeadlineExceeded }

	if _, cmd := mod.handleSlash("/clear"); cmd != nil {
		t.Fatalf("unexpected cmd for clear error: %v", cmd)
	}
	if len(mod.turns) == 0 || !mod.turns[len(mod.turns)-1].isMeta {
		t.Fatalf("expected meta turn after clear error: %+v", mod.turns)
	}

	if _, cmd := mod.handleSlash("/exit"); cmd == nil || !mod.quitting {
		t.Fatalf("expected quit command from /exit, cmd=%v quitting=%v", cmd, mod.quitting)
	}
}
