package blades

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/session"
)

// Fork creates a new default LLM agent from an existing default LLM agent.
func Fork(agent Agent, opts ...AgentOption) (Agent, error) {
	base, ok := agent.(*llmAgent)
	if !ok {
		return nil, ErrAgentNotForkable
	}
	fork := base.clone()
	fork.name = base.name + "-fork"
	for _, opt := range opts {
		opt(fork)
	}
	if fork.provider == nil {
		return nil, ErrModelProviderRequired
	}
	return fork, nil
}

// ForkSummarizer creates a compact.Summarizer backed by a compact-safe fork of
// the currently running default LLM agent.
func ForkSummarizer(opts ...AgentOption) compact.Summarizer {
	return forkSummarizer{opts: opts}
}

type forkSummarizer struct {
	opts []AgentOption
}

func (s forkSummarizer) Summarize(ctx context.Context, req compact.SummaryRequest) (string, error) {
	base, ok := runtimeAgentFromContext(ctx)
	if !ok {
		return "", ErrAgentNotStarted
	}
	agent, err := s.newAgent(base)
	if err != nil {
		return "", err
	}
	summaryCtx := session.NewContext(ctx, session.NewSession())
	result, err := NewRunner(agent).Run(summaryCtx, event.NewPromptText(formatSummaryPrompt(req)))
	if err != nil {
		return "", err
	}
	if result.Err != nil {
		return "", result.Err
	}
	return result.Text(), nil
}

func (s forkSummarizer) newAgent(base *llmAgent) (Agent, error) {
	agent := &llmAgent{
		name:        base.name + "-compact",
		description: "compact summary agent",
		provider:    base.provider,
	}
	for _, opt := range s.opts {
		opt(agent)
	}
	if agent.provider == nil {
		return nil, ErrModelProviderRequired
	}
	return agent, nil
}

func formatSummaryPrompt(req compact.SummaryRequest) string {
	var b strings.Builder
	b.WriteString("Summarize the following conversation history for future model context.\n")
	b.WriteString("Preserve user goals, decisions, constraints, tool results, unresolved tasks, and important facts.\n")
	b.WriteString("Do not invent facts. Keep the summary concise and self-contained.")
	if req.MaxTokens > 0 {
		fmt.Fprintf(&b, "\nTarget maximum summary tokens: %d.", req.MaxTokens)
	}
	b.WriteString("\n\nTranscript:\n")
	for i, msg := range req.Messages {
		fmt.Fprintf(&b, "\n<message index=%d role=%q>\n", i, messageRole(msg))
		b.WriteString(formatMessageParts(msg))
		b.WriteString("\n</message>\n")
	}
	return b.String()
}

func messageRole(msg *model.Message) model.Role {
	if msg == nil {
		return ""
	}
	return msg.Role
}

func formatMessageParts(msg *model.Message) string {
	if msg == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case content.Text:
			b.WriteString(p.Text)
		case content.Thinking:
			b.WriteString("[thinking] ")
			b.WriteString(p.Text)
		case content.ToolUse:
			fmt.Fprintf(&b, "[tool_use id=%q name=%q input=%s]", p.ID, p.Name, string(p.Input))
		case content.ToolResult:
			fmt.Fprintf(&b, "[tool_result id=%q name=%q error=%t]\n", p.ID, p.Name, p.IsError)
			b.WriteString(formatParts(p.Parts))
		case content.FilePart:
			fmt.Fprintf(&b, "[file uri=%q mime=%q name=%q]", p.URI, p.MIME, p.Filename)
		case content.FileRefPart:
			fmt.Fprintf(&b, "[file_ref id=%q mime=%q]", p.ID, p.MIME)
		case content.DataPart:
			fmt.Fprintf(&b, "[data mime=%q name=%q bytes=%d]", p.MIME, p.Filename, len(p.Bytes))
		default:
			data, _ := json.Marshal(p)
			b.Write(data)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func formatParts(parts []content.Part) string {
	return formatMessageParts(&model.Message{Parts: parts})
}
