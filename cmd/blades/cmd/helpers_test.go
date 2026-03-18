package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/recipe"
	bladeskills "github.com/go-kratos/blades/skills"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	clichi "github.com/go-kratos/blades/cmd/blades/internal/channel/cli"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

type fixedReplyAgent struct {
	text string
}

type toolStreamingAgent struct{}

func (a *fixedReplyAgent) Name() string { return "fixed-reply" }

func (a *fixedReplyAgent) Description() string { return "" }

func (a *fixedReplyAgent) Run(context.Context, *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		yield(blades.AssistantMessage(a.text), nil)
	}
}

func (a *toolStreamingAgent) Name() string { return "tool-streaming" }

func (a *toolStreamingAgent) Description() string { return "" }

func (a *toolStreamingAgent) Run(context.Context, *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		request := `{"q":"same"}`
		yield(&blades.Message{
			Role:   blades.RoleAssistant,
			Status: blades.StatusInProgress,
			Parts: []blades.Part{
				blades.NewToolPart("", "search", request),
				blades.NewToolPart("", "search", request),
			},
		}, nil)
		yield(&blades.Message{
			Role:   blades.RoleAssistant,
			Status: blades.StatusCompleted,
			Parts: []blades.Part{
				blades.TextPart{Text: "done"},
				blades.ToolPart{Name: "search", Request: request, Response: "result-1", Completed: true},
				blades.ToolPart{Name: "search", Request: request, Response: "result-2", Completed: true},
			},
		}, nil)
	}
}

type eventCaptureWriter struct {
	events []channel.Event
}

type historyAwareMathAgent struct{}

func (w *eventCaptureWriter) WriteText(string) {}

func (w *eventCaptureWriter) WriteEvent(e channel.Event) {
	w.events = append(w.events, e)
}

func (a *historyAwareMathAgent) Name() string { return "history-aware-math" }

func (a *historyAwareMathAgent) Description() string { return "" }

func (a *historyAwareMathAgent) Run(_ context.Context, inv *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		reply := "missing context"
		switch got := inv.Message.Text(); got {
		case "1+1=?":
			reply = "2"
		case "再加一呢":
			history := inv.Session.History()
			if len(history) >= 3 && history[0].Text() == "1+1=?" && history[1].Text() == "2" && history[2].Text() == "再加一呢" {
				reply = "3"
			}
		}

		msg := blades.NewAssistantMessage(blades.StatusCompleted)
		msg.Parts = append(msg.Parts, blades.TextPart{Text: reply})
		yield(msg, nil)
	}
}

func TestInitCreatesLoadableBuiltInSkills(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	ws := workspace.New(homeDir)
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	skillList, err := bladeskills.NewFromDir(ws.SkillsDir())
	if err != nil {
		t.Fatalf("load built-in skills: %v", err)
	}

	for _, skill := range skillList {
		if skill.Name() == "blades-cron" {
			return
		}
	}

	t.Fatalf("expected built-in skill %q in %s", "blades-cron", ws.SkillsDir())
}

func TestDefaultExecWorkingDirUsesWorkspaceDir(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workspaceDir := t.TempDir()
	ws := workspace.NewWithWorkspace(homeDir, workspaceDir)

	// defaultExecWorkingDir should return the workspace directory, not the home directory
	if got, want := appcore.DefaultExecWorkingDir(ws), workspaceDir; got != want {
		t.Fatalf("default exec working dir = %q, want %q", got, want)
	}
}

func TestDefaultExecWorkingDirFallsBackToDot(t *testing.T) {
	t.Parallel()

	if got, want := appcore.DefaultExecWorkingDir(nil), "."; got != want {
		t.Fatalf("default exec working dir for nil workspace = %q, want %q", got, want)
	}
}

func TestBuildToolRegistryRegistersRecipeTools(t *testing.T) {
	t.Parallel()

	registry := appcore.BuildToolRegistry(bldtools.DefaultExecConfig(t.TempDir()), nil)

	for _, name := range []string{"exec", "cron", "exit"} {
		if _, err := registry.Resolve(name); err != nil {
			t.Fatalf("expected tool %q to be registered: %v", name, err)
		}
	}
}

func TestBuildMiddlewareRegistryRegistersRetry(t *testing.T) {
	t.Parallel()

	registry := appcore.BuildMiddlewareRegistry()
	if _, err := registry.Resolve("retry", map[string]any{"attempts": 3}); err != nil {
		t.Fatalf("expected retry middleware to resolve: %v", err)
	}
}

func TestLoadAgentSpecDefaultMatchesRecipeSpec(t *testing.T) {
	t.Parallel()

	ws := workspace.New(t.TempDir())
	spec, err := appcore.LoadAgentSpec(ws)
	if err != nil {
		t.Fatalf("loadAgentSpec: %v", err)
	}

	if err := recipe.Validate(spec); err != nil {
		t.Fatalf("default spec should be valid recipe spec: %v", err)
	}
	if got, want := spec.Tools, []string{"exec", "cron"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("default spec tools = %v, want %v", got, want)
	}
}

func TestBuildRunnerSupportsRecipeToolsAndMiddlewares(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	ws := workspace.New(homeDir)
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:     "openai",
				Provider: "openai",
				Models:   []string{"gpt-4o"},
				APIKey:   "test-key",
			},
		},
	}

	if _, err := appcore.BuildRunner(cfg, ws, nil); err != nil {
		t.Fatalf("buildRunner should accept recipe-spec agent.yaml template: %v", err)
	}
}

func TestMakeTriggerPersistsSession(t *testing.T) {
	t.Parallel()

	agent := &fixedReplyAgent{text: "saved reply"}
	runner := blades.NewRunner(agent)

	sessionsDir := t.TempDir()
	sessMgr := session.NewManager(sessionsDir)
	trigger := appcore.NewTurnExecutor(runner, sessMgr, appcore.TurnOptions{}).Trigger()

	reply, err := trigger(context.Background(), "cron-session", "hello")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if reply != "saved reply" {
		t.Fatalf("reply = %q, want %q", reply, "saved reply")
	}

	reloaded, err := session.NewManager(sessionsDir).Get("cron-session")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got := len(reloaded.History()); got == 0 {
		t.Fatalf("expected persisted session history, got %d messages", got)
	}
}

func TestExecuteTurnTracksDuplicateToolRequestsSeparately(t *testing.T) {
	t.Parallel()

	runner := blades.NewRunner(&toolStreamingAgent{})
	sessMgr := session.NewManager(t.TempDir())
	writer := &eventCaptureWriter{}

	reply, err := appcore.NewTurnExecutor(runner, sessMgr, appcore.TurnOptions{
		Writer: writer,
	}).Run(context.Background(), "dup-tools", "hello")
	if err != nil {
		t.Fatalf("executeTurn: %v", err)
	}
	if reply != "done" {
		t.Fatalf("reply = %q, want %q", reply, "done")
	}
	if got := len(writer.events); got != 4 {
		t.Fatalf("event count = %d, want 4", got)
	}
	if writer.events[0].ID == writer.events[1].ID {
		t.Fatalf("duplicate tool starts share ID %q", writer.events[0].ID)
	}
	if writer.events[2].ID != writer.events[0].ID {
		t.Fatalf("first tool end ID = %q, want %q", writer.events[2].ID, writer.events[0].ID)
	}
	if writer.events[3].ID != writer.events[1].ID {
		t.Fatalf("second tool end ID = %q, want %q", writer.events[3].ID, writer.events[1].ID)
	}
}

func TestSimpleChatChannelPreservesContextAcrossTurns(t *testing.T) {
	t.Parallel()

	runner := blades.NewRunner(&historyAwareMathAgent{})
	sessionsDir := t.TempDir()
	sessMgr := session.NewManager(sessionsDir)
	handler := appcore.NewTurnExecutor(runner, sessMgr, appcore.TurnOptions{}).StreamHandler()

	var stdout, stderr bytes.Buffer
	input := bytes.NewBufferString("1+1=?\n再加一呢\n")
	ch := clichi.New("chat-session",
		clichi.WithNoAltScreen(),
		clichi.WithIO(input, &stdout, &stderr),
	)

	if err := ch.Start(context.Background(), handler); err != nil {
		t.Fatalf("simple chat start: %v", err)
	}

	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q", got)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("2")) || !bytes.Contains([]byte(got), []byte("3")) || bytes.Contains([]byte(got), []byte("missing context")) {
		t.Fatalf("stdout = %q, want preserved context across both turns", got)
	}

	reloaded, err := session.NewManager(sessionsDir).Get("chat-session")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got := len(reloaded.History()); got != 4 {
		t.Fatalf("history len = %d, want 4", got)
	}
}
