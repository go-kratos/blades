// Package command provides a unified command processing system for all channels.
package command

import (
	"context"
	"fmt"
	"strings"
)

// Command represents a slash command that can be executed.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Handler     func(ctx context.Context, args []string, env *Environment) (*Result, error)
}

// Environment provides context and capabilities to command handlers.
type Environment struct {
	// SessionID is the current session identifier
	SessionID string

	// ReloadFunc reloads skills/configuration if available
	ReloadFunc func() error

	// StopFunc stops the current streaming response if available
	StopFunc func() error

	// SwitchSessionFunc switches to a different session
	SwitchSessionFunc func(sessionID string) error

	// ClearFunc clears the conversation history
	ClearFunc func() error

	// Processor is the command processor (optional). When set, /help lists its registered commands.
	Processor *Processor

	// Custom data that channels can provide
	Custom map[string]interface{}
}

// Result represents the outcome of a command execution.
type Result struct {
	// Message to display to the user
	Message string

	// IsError indicates if this is an error result
	IsError bool

	// ShouldQuit indicates the application should exit
	ShouldQuit bool

	// Metadata for channel-specific handling
	Metadata map[string]interface{}
}

// Processor handles command parsing and execution.
type Processor struct {
	commands map[string]*Command
}

// NewProcessor creates a new command processor with default commands.
func NewProcessor() *Processor {
	p := &Processor{
		commands: make(map[string]*Command),
	}
	p.registerDefaults()
	return p
}

// registerDefaults registers the built-in commands.
func (p *Processor) registerDefaults() {
	p.Register(&Command{
		Name:        "help",
		Description: "Show available commands",
		Usage:       "/help",
		Handler:     helpHandler,
	})

	p.Register(&Command{
		Name:        "reload",
		Description: "Hot-reload skills and configuration",
		Usage:       "/reload",
		Handler:     reloadHandler,
	})

	p.Register(&Command{
		Name:        "stop",
		Description: "Stop the current response (agent stops; you can keep chatting)",
		Usage:       "/stop",
		Handler:     stopHandler,
	})

	p.Register(&Command{
		Name:        "session",
		Description: "Show or switch session",
		Usage:       "/session [id]",
		Handler:     sessionHandler,
	})

	p.Register(&Command{
		Name:        "clear",
		Description: "Clear conversation history",
		Usage:       "/clear",
		Handler:     clearHandler,
	})

	p.Register(&Command{
		Name:        "exit",
		Aliases:     []string{"quit"},
		Description: "Exit the application",
		Usage:       "/exit",
		Handler:     exitHandler,
	})
}

// Register adds a command to the processor.
func (p *Processor) Register(cmd *Command) {
	p.commands[cmd.Name] = cmd
	for _, alias := range cmd.Aliases {
		p.commands[alias] = cmd
	}
}

// Process parses and executes a command line.
// Returns nil if the line is not a command (doesn't start with /).
func (p *Processor) Process(ctx context.Context, line string, env *Environment) (*Result, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return nil, nil
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, nil
	}

	cmdName := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	cmd, ok := p.commands[cmdName]
	if !ok {
		return &Result{
			Message: fmt.Sprintf("Unknown command: %q. Type /help for available commands.", parts[0]),
			IsError: true,
		}, nil
	}

	return cmd.Handler(ctx, args, env)
}

// GetCommands returns all registered commands (deduplicated by name).
func (p *Processor) GetCommands() []*Command {
	seen := make(map[string]bool)
	var cmds []*Command
	for _, cmd := range p.commands {
		if !seen[cmd.Name] {
			seen[cmd.Name] = true
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// ── Built-in command handlers ─────────────────────────────────────────────────

func helpHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	var b strings.Builder
	b.WriteString("## Available Commands\n\n")
	b.WriteString("| Command | Description |\n")
	b.WriteString("|---------|-------------|\n")

	if env != nil && env.Processor != nil {
		for _, cmd := range env.Processor.GetCommands() {
			usage := cmd.Usage
			if usage == "" {
				usage = "/" + cmd.Name
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s |\n", usage, cmd.Description))
		}
	} else {
		commands := []struct {
			name, desc string
		}{
			{"/help", "Show this help"},
			{"/reload", "Hot-reload skills"},
			{"/stop", "Stop current response (keep chatting)"},
			{"/session [id]", "Show or switch session"},
			{"/clear", "Clear conversation"},
			{"/exit", "Quit"},
		}
		for _, cmd := range commands {
			b.WriteString(fmt.Sprintf("| `%s` | %s |\n", cmd.name, cmd.desc))
		}
	}

	// Channel-specific content (e.g. CLI keyboard shortcuts) can be appended via Custom["helpSuffix"].
	if env != nil && env.Custom != nil {
		if s, ok := env.Custom["helpSuffix"].(string); ok && s != "" {
			b.WriteString("\n")
			b.WriteString(s)
		}
	}

	return &Result{Message: b.String()}, nil
}

func reloadHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	if env == nil {
		return &Result{
			Message: "(reload not available - no environment)",
			IsError: true,
		}, nil
	}
	if env.ReloadFunc == nil {
		return &Result{
			Message: "(reload not configured)",
		}, nil
	}

	if err := env.ReloadFunc(); err != nil {
		return &Result{
			Message: fmt.Sprintf("Reload failed: %v", err),
			IsError: true,
		}, nil
	}

	return &Result{
		Message: "✓ Skills reloaded successfully",
	}, nil
}

func stopHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	if env == nil {
		return &Result{
			Message: "(stop not available - no environment)",
			IsError: true,
		}, nil
	}
	if env.StopFunc == nil {
		return &Result{
			Message: "(stop not available - no active response)",
		}, nil
	}

	if err := env.StopFunc(); err != nil {
		return &Result{
			Message: fmt.Sprintf("Stop failed: %v", err),
			IsError: true,
		}, nil
	}

	return &Result{
		Message: "⏹ Response stopped. You can continue the conversation.",
	}, nil
}

func sessionHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	if env == nil {
		return &Result{
			Message: "(session not available - no environment)",
			IsError: true,
		}, nil
	}
	if len(args) == 0 {
		return &Result{
			Message: fmt.Sprintf("Current session: %s", env.SessionID),
		}, nil
	}

	newSessionID := args[0]
	if env.SwitchSessionFunc != nil {
		if err := env.SwitchSessionFunc(newSessionID); err != nil {
			return &Result{
				Message: fmt.Sprintf("Failed to switch session: %v", err),
				IsError: true,
			}, nil
		}
	}

	return &Result{
		Message: fmt.Sprintf("Switched to session: %s", newSessionID),
	}, nil
}

func clearHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	if env == nil {
		return &Result{
			Message: "(clear not available - no environment)",
			IsError: true,
		}, nil
	}
	if env.ClearFunc == nil {
		return &Result{
			Message: "(clear not available)",
		}, nil
	}

	if err := env.ClearFunc(); err != nil {
		return &Result{
			Message: fmt.Sprintf("Clear failed: %v", err),
			IsError: true,
		}, nil
	}

	return &Result{
		Message: "✓ Conversation cleared",
	}, nil
}

func exitHandler(ctx context.Context, args []string, env *Environment) (*Result, error) {
	return &Result{
		Message:    "Bye! 👋",
		ShouldQuit: true,
	}, nil
}
