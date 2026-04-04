package command

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProcessor_Process(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		env         *Environment
		expectError bool
		expectQuit  bool
		expectMsg   string
	}{
		{
			name:        "non-command line",
			line:        "hello world",
			expectError: false,
			expectMsg:   "",
		},
		{
			name:        "help command",
			line:        "/help",
			expectError: false,
			expectMsg:   "Available Commands",
		},
		{
			name:        "exit command",
			line:        "/exit",
			expectError: false,
			expectQuit:  true,
			expectMsg:   "Bye",
		},
		{
			name:        "quit alias",
			line:        "/quit",
			expectError: false,
			expectQuit:  true,
			expectMsg:   "Bye",
		},
		{
			name:        "unknown command",
			line:        "/unknown",
			expectError: false,
			expectMsg:   "Unknown command",
		},
		{
			name: "session command show",
			line: "/session",
			env: &Environment{
				SessionID: "test-session",
			},
			expectError: false,
			expectMsg:   "test-session",
		},
		{
			name: "session command switch",
			line: "/session new-session",
			env: &Environment{
				SessionID: "old-session",
				SwitchSessionFunc: func(id string) error {
					return nil
				},
			},
			expectError: false,
			expectMsg:   "new-session",
		},
		{
			name: "session command switch unavailable",
			line: "/session new-session",
			env: &Environment{
				SessionID: "old-session",
			},
			expectError: false,
			expectMsg:   "not available",
		},
		{
			name: "stop command",
			line: "/stop",
			env: &Environment{
				StopFunc: func() error { return nil },
			},
			expectError: false,
			expectMsg:   "stopped",
		},
		{
			name: "clear command",
			line: "/clear",
			env: &Environment{
				ClearFunc: func() error { return nil },
			},
			expectError: false,
			expectMsg:   "cleared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProcessor()
			ctx := context.Background()

			if tt.env == nil {
				tt.env = &Environment{}
			}

			result, err := p.Process(ctx, tt.line, tt.env)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectMsg == "" {
				if result != nil {
					t.Fatalf("expected nil result, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected result, got nil")
			}

			if tt.expectQuit && !result.ShouldQuit {
				t.Errorf("expected ShouldQuit=true, got false")
			}

			if !tt.expectQuit && result.ShouldQuit {
				t.Errorf("expected ShouldQuit=false, got true")
			}

			// Check if expected message is in the result
			if tt.expectMsg != "" && !contains(result.Message, tt.expectMsg) {
				t.Errorf("expected message to contain %q, got %q", tt.expectMsg, result.Message)
			}
		})
	}
}

func TestProcessor_GetCommands(t *testing.T) {
	p := NewProcessor()
	cmds := p.GetCommands()

	if len(cmds) == 0 {
		t.Fatal("expected commands, got none")
	}

	// Check that we have the expected commands
	cmdNames := make(map[string]bool)
	for _, cmd := range cmds {
		cmdNames[cmd.Name] = true
	}

	expected := []string{"help", "stop", "session", "clear", "exit"}
	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("expected command %q not found", name)
		}
	}
}

func TestBuiltinHandlersAdditionalBranches(t *testing.T) {
	t.Parallel()

	help, err := helpHandler(context.Background(), nil, &Environment{
		Processor: NewProcessor(),
		Custom:    map[string]any{"helpSuffix": "\nextra help"},
	})
	if err != nil {
		t.Fatalf("helpHandler: %v", err)
	}
	if !strings.Contains(help.Message, "extra help") {
		t.Fatalf("help message = %q", help.Message)
	}

	stop, err := stopHandler(context.Background(), nil, nil)
	if err != nil || !stop.IsError {
		t.Fatalf("stopHandler(nil env) = %+v, %v", stop, err)
	}

	stop, err = stopHandler(context.Background(), nil, &Environment{})
	if err != nil || stop.IsError {
		t.Fatalf("stopHandler(no stop func) = %+v, %v", stop, err)
	}

	stop, err = stopHandler(context.Background(), nil, &Environment{
		StopFunc: func() error { return errors.New("boom") },
	})
	if err != nil || !stop.IsError || !strings.Contains(stop.Message, "boom") {
		t.Fatalf("stopHandler(error) = %+v, %v", stop, err)
	}

	clear, err := clearHandler(context.Background(), nil, nil)
	if err != nil || !clear.IsError {
		t.Fatalf("clearHandler(nil env) = %+v, %v", clear, err)
	}

	clear, err = clearHandler(context.Background(), nil, &Environment{
		ClearFunc: func() error { return errors.New("wipe failed") },
	})
	if err != nil || !clear.IsError || !strings.Contains(clear.Message, "wipe failed") {
		t.Fatalf("clearHandler(error) = %+v, %v", clear, err)
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || strings.Contains(s, substr)
}
