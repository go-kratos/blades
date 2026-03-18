package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
)

// CardWriter implements channel.Writer using Lark card messages for streaming.
type CardWriter struct {
	ctx       context.Context
	client    *lark.Client
	messageID string
	chatID    string
	chatType  string

	mu          sync.Mutex
	textBuf     strings.Builder
	tools       []toolInfo
	lastSentLen int
	cardID      string

	flushInterval time.Duration
	flushTimer    *time.Timer
	done          chan struct{}
	closeOnce     sync.Once

	// Reaction tracking
	reactionID   string
	reactionSent bool
}

type toolInfo struct {
	id       string
	name     string
	input    string
	output   string
	status   string // "running", "done", "error"
	started  bool
	finished bool
}

// NewCardWriter creates a new CardWriter for streaming output.
func NewCardWriter(ctx context.Context, client *lark.Client, messageID, chatID, chatType string) *CardWriter {
	w := &CardWriter{
		ctx:           ctx,
		client:        client,
		messageID:     messageID,
		chatID:        chatID,
		chatType:      chatType,
		flushInterval: 300 * time.Millisecond,
		done:          make(chan struct{}),
	}
	return w
}

// WriteText implements channel.Writer.
func (w *CardWriter) WriteText(chunk string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.textBuf.WriteString(chunk)

	// Send OK reaction on first write
	if !w.reactionSent {
		w.reactionSent = true
		go w.sendReaction("👍")
	}

	if w.flushTimer == nil {
		w.flushTimer = time.AfterFunc(w.flushInterval, w.flush)
	} else {
		w.flushTimer.Reset(w.flushInterval)
	}
}

// WriteEvent implements channel.Writer.
func (w *CardWriter) WriteEvent(e channel.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch e.Kind {
	case channel.EventToolStart:
		w.tools = append(w.tools, toolInfo{
			id:     e.ID,
			name:   e.Name,
			input:  e.Input,
			status: "running",
		})
	case channel.EventToolEnd:
		for i := range w.tools {
			if w.tools[i].id == e.ID && w.tools[i].status == "running" {
				w.tools[i].output = e.Output
				w.tools[i].status = "done"
				if isToolErrorOutput(e.Output) {
					w.tools[i].status = "error"
				}
				break
			}
		}
	}

	// Immediate flush for events
	go w.flushSync()
}

// flush triggers a sync flush.
func (w *CardWriter) flush() {
	select {
	case <-w.done:
		return
	case <-w.ctx.Done():
		return
	default:
	}

	w.flushSync()
}

// flushSync sends or updates the card message.
func (w *CardWriter) flushSync() {
	w.mu.Lock()
	defer w.mu.Unlock()

	textContent := w.textBuf.String()
	if len(textContent) <= w.lastSentLen && len(w.tools) == 0 {
		return
	}

	cardJSON := w.buildCard(textContent)

	if w.cardID == "" {
		if err := w.sendCard(cardJSON); err == nil {
			w.lastSentLen = len(textContent)
		}
	} else {
		if err := w.updateCard(cardJSON); err == nil {
			w.lastSentLen = len(textContent)
		}
	}
}

// buildCard creates a compact Lark card with tool markdown blocks followed by text.
func (w *CardWriter) buildCard(textContent string) string {
	var elements []map[string]interface{}

	if len(w.tools) > 0 {
		for _, tool := range w.tools {
			elements = append(elements, buildToolPanel(tool))
		}
	}

	if textContent != "" {
		elements = append(elements, map[string]interface{}{
			"tag":     "markdown",
			"content": textContent + "▌",
		})
	}

	simpleCard := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"elements": elements,
	}

	cardJSON, _ := json.Marshal(simpleCard)
	return string(cardJSON)
}

// sendCard creates a new card message.
func (w *CardWriter) sendCard(cardJSON string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeInteractive).
			ReceiveId(w.chatID).
			Content(cardJSON).
			Build()).
		Build()

	resp, err := w.client.Im.Message.Create(w.ctx, req)
	if err != nil {
		return fmt.Errorf("create card: %w", err)
	}

	if resp.Data != nil && resp.Data.MessageId != nil {
		w.cardID = larkcore.StringValue(resp.Data.MessageId)
	}

	return nil
}

// updateCard updates an existing card message.
func (w *CardWriter) updateCard(cardJSON string) error {
	if w.cardID == "" {
		return w.sendCard(cardJSON)
	}

	req := larkim.NewPatchMessageReqBuilder().
		MessageId(w.cardID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardJSON).
			Build()).
		Build()

	_, err := w.client.Im.Message.Patch(w.ctx, req)
	if err != nil {
		return fmt.Errorf("update card: %w", err)
	}

	return nil
}

// sendReaction sends a reaction to the original message.
func (w *CardWriter) sendReaction(emoji string) error {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(w.messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(&larkim.Emoji{
				EmojiType: larkcore.StringPtr(emoji),
			}).
			Build()).
		Build()

	resp, err := w.client.Im.MessageReaction.Create(w.ctx, req)
	if err != nil {
		return err
	}

	// Save reaction ID for later deletion
	if resp.Data != nil && resp.Data.ReactionId != nil {
		w.mu.Lock()
		w.reactionID = larkcore.StringValue(resp.Data.ReactionId)
		w.mu.Unlock()
	}

	return nil
}

// deleteReaction removes the reaction from the message.
func (w *CardWriter) deleteReaction() error {
	if w.reactionID == "" {
		return nil
	}

	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(w.messageID).
		ReactionId(w.reactionID).
		Build()

	_, err := w.client.Im.MessageReaction.Delete(w.ctx, req)
	return err
}

// Close stops the flush timer, removes reaction, and sends final content.
// It is idempotent and safe to call multiple times.
func (w *CardWriter) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.done)
		if w.flushTimer != nil {
			w.flushTimer.Stop()
		}

		// Remove OK reaction
		if w.reactionSent {
			_ = w.deleteReaction()
		}

		// Final card without cursor
		w.mu.Lock()
		textContent := strings.TrimSuffix(w.textBuf.String(), "▌")
		cardJSON := w.buildFinalCard(textContent)
		w.mu.Unlock()

		err = w.updateCard(cardJSON)
	})
	return err
}

// buildFinalCard creates the final card without the streaming cursor.
func (w *CardWriter) buildFinalCard(textContent string) string {
	var elements []map[string]interface{}

	if len(w.tools) > 0 {
		for _, tool := range w.tools {
			elements = append(elements, buildToolPanel(tool))
		}
	}

	if textContent != "" {
		elements = append(elements, map[string]interface{}{
			"tag":     "markdown",
			"content": textContent,
		})
	}

	simpleCard := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"elements": elements,
	}

	cardJSON, _ := json.Marshal(simpleCard)
	return string(cardJSON)
}

func buildToolPanel(tool toolInfo) map[string]interface{} {
	_, _, statusText, _ := toolPanelMeta(tool.status)
	header := strings.TrimSpace(fmt.Sprintf("**%s**", tool.name))
	if statusText != "" {
		header += " " + statusText
	}

	return map[string]interface{}{
		"tag":     "markdown",
		"content": header + "\n" + buildToolCodeBlock(tool, 1200),
	}
}

func toolPanelMeta(status string) (icon, template, statusText, collapsedText string) {
	switch status {
	case "running":
		return "", "", "running", ""
	case "error":
		return "", "", "error", ""
	default:
		return "", "", "", ""
	}
}

func buildToolCodeBlock(tool toolInfo, maxOutput int) string {
	command, workingDir := parseToolCommand(tool.input)
	output := strings.TrimSpace(tool.output)
	if maxOutput > 0 && len(output) > maxOutput {
		output = output[:maxOutput] + "...(truncated)"
	}

	var body strings.Builder
	if command != "" {
		if workingDir != "" {
			body.WriteString("# cwd: ")
			body.WriteString(workingDir)
			body.WriteString("\n")
		}
		body.WriteString("$ ")
		body.WriteString(command)
	} else if rawInput := strings.TrimSpace(tool.input); rawInput != "" {
		body.WriteString("# input\n")
		body.WriteString(rawInput)
	} else {
		body.WriteString("# no input")
	}

	if output != "" {
		body.WriteString("\n\n")
		body.WriteString(output)
	} else {
		body.WriteString("\n\n")
		if tool.status == "running" {
			body.WriteString("[running]")
		} else {
			body.WriteString("[no output]")
		}
	}

	return "```bash\n" + body.String() + "\n```"
}

func parseToolCommand(input string) (command, workingDir string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}

	var req struct {
		Command    string `json:"command"`
		WorkingDir string `json:"working_dir"`
	}
	if err := json.Unmarshal([]byte(trimmed), &req); err == nil {
		return strings.TrimSpace(req.Command), strings.TrimSpace(req.WorkingDir)
	}

	return "", ""
}

func isToolErrorOutput(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, "error:") {
		return true
	}
	return strings.Contains(normalized, "\nexit:") || strings.HasPrefix(normalized, "exit:")
}
