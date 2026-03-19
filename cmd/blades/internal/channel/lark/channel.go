// Package lark implements a Lark (Feishu) channel for the blades agent.
// It uses WebSocket mode for receiving messages and supports streaming output.
package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

const channelName = "lark"

// Channel implements channel.Channel for Lark/Feishu using WebSocket mode.
type Channel struct {
	appID             string
	appSecret         string
	encryptKey        string
	verificationToken string
	client            *lark.Client
	wsClient          *larkws.Client

	// session management
	sessions sync.Map // messageID -> *sessionState

	// message de-duplication for retried/replayed events
	processedMessages sync.Map // messageID -> first seen time.Time
	dedupeTTL         time.Duration
	pruneMu           sync.Mutex
	lastPruneAt       time.Time

	// command handling
	cmdProc      *command.Processor
	stop         func() error
	clearSession func(string) error

	debug  bool // when true, log received messages and watch events
	output io.Writer
	logf   func(string, ...any)
	now    func() time.Time
}

type sessionState struct {
	sessionID string
	chatType  string // "p2p" or "group"
}

// Option configures a Channel.
type Option func(*Channel)

// WithAppID sets the Lark app ID.
func WithAppID(appID string) Option {
	return func(c *Channel) { c.appID = appID }
}

// WithAppSecret sets the Lark app secret.
func WithAppSecret(secret string) Option {
	return func(c *Channel) { c.appSecret = secret }
}

// WithEncryptKey sets the encryption key for message decryption.
func WithEncryptKey(key string) Option {
	return func(c *Channel) { c.encryptKey = key }
}

// WithVerificationToken sets the verification token.
func WithVerificationToken(token string) Option {
	return func(c *Channel) { c.verificationToken = token }
}

// WithDebug enables logging of received messages and watch events.
func WithDebug(enabled bool) Option {
	return func(c *Channel) { c.debug = enabled }
}

// WithStop sets the function called when the user issues /stop.
func WithStop(fn func() error) Option {
	return func(c *Channel) { c.stop = fn }
}

// WithClearSession sets the function called when the user issues /clear.
func WithClearSession(fn func(string) error) Option {
	return func(c *Channel) { c.clearSession = fn }
}

// WithOutput sets where startup/status messages are written.
func WithOutput(w io.Writer) Option {
	return func(c *Channel) { c.output = w }
}

// WithLogf overrides diagnostic logging.
func WithLogf(fn func(string, ...any)) Option {
	return func(c *Channel) { c.logf = fn }
}

// WithNow overrides the wall clock used for dedupe bookkeeping.
func WithNow(fn func() time.Time) Option {
	return func(c *Channel) { c.now = fn }
}

// New creates a new Lark channel.
func New(opts ...Option) *Channel {
	c := &Channel{
		dedupeTTL: 2 * time.Minute,
		cmdProc:   command.NewProcessor(),
		output:    io.Discard,
		logf:      log.Printf,
		now:       time.Now,
	}
	for _, o := range opts {
		o(c)
	}

	// Create Lark client
	c.client = lark.NewClient(c.appID, c.appSecret)
	return c
}

// Name implements channel.Channel.
func (c *Channel) Name() string { return channelName }

// Start implements channel.Channel.
// It starts a WebSocket client to receive events from Lark.
func (c *Channel) Start(ctx context.Context, handler channel.StreamHandler) error {
	// Create event dispatcher
	dispatcher := larkdispatcher.NewEventDispatcher(c.verificationToken, c.encryptKey).
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return c.handleMessage(ctx, event, handler)
		})

	// Create WebSocket client
	c.wsClient = larkws.NewClient(
		c.appID,
		c.appSecret,
		larkws.WithEventHandler(dispatcher),
		larkws.WithAutoReconnect(true),
	)

	fmt.Fprintln(c.output, "Lark channel connecting via WebSocket...")
	return c.wsClient.Start(ctx)
}

// handleMessage processes a received message.
func (c *Channel) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1, handler channel.StreamHandler) error {
	if event == nil || event.Event == nil {
		return nil
	}
	message := event.Event.Message
	if message == nil {
		return nil
	}

	messageID := strings.TrimSpace(larkcore.StringValue(message.MessageId))
	if messageID == "" {
		return nil
	}
	if !c.markMessageIfNew(messageID, c.nowTime()) {
		return nil
	}

	chatID := larkcore.StringValue(message.ChatId)
	chatType := larkcore.StringValue(message.ChatType)

	// Get message content
	content := strings.TrimSpace(c.parseMessageContent(message))
	if content == "" {
		return nil
	}

	// Get or create session - use chatID as sessionID so messages in the same chat share history
	sessionID := c.getOrCreateSession(chatID, chatType)

	if c.debug {
		c.printf("lark: received message chat=%s type=%s session=%s content=%q", chatID, chatType, sessionID, truncForLog(content, 200))
	}

	// Check if this is a command
	if strings.HasPrefix(content, "/") {
		return c.handleCommand(ctx, content, sessionID, chatID)
	}

	// Create a card writer for streaming response
	writer := NewCardWriter(ctx, c.client, messageID, chatID, chatType)
	defer func() {
		if err := writer.Close(); err != nil {
			c.printf("lark: failed to close writer: %v", err)
		}
	}()

	// Process the message with the handler
	if handler == nil {
		return fmt.Errorf("stream handler is nil")
	}
	_, err := handler(ctx, sessionID, content, writer)
	if err != nil {
		// Send error message
		c.sendErrorMessage(ctx, chatID, err.Error())
	}

	return err
}

// handleCommand processes a slash command in Lark.
func (c *Channel) handleCommand(ctx context.Context, line string, sessionID string, chatID string) error {
	env := &command.Environment{
		SessionID: sessionID,
		StopFunc:  c.stop,
		SwitchSessionFunc: func(newSessionID string) error {
			c.setSession(chatID, newSessionID)
			return nil
		},
		ClearFunc: func() error {
			if c.clearSession != nil {
				return c.clearSession(sessionID)
			}
			return nil
		},
		Processor: c.cmdProc,
	}

	result, err := c.cmdProc.Process(ctx, line, env)
	if err != nil {
		return c.sendErrorMessage(ctx, chatID, fmt.Sprintf("Command error: %v", err))
	}

	if result != nil {
		msg := result.Message
		if result.IsError {
			msg = "❌ " + msg
		}
		return c.sendTextMessage(ctx, chatID, msg)
	}

	return nil
}

// sendTextMessage sends a plain text message to a chat.
func (c *Channel) sendTextMessage(ctx context.Context, chatID, text string) error {
	if chatID == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	content, _ := json.Marshal(map[string]string{"text": strings.TrimSpace(text)})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			ReceiveId(chatID).
			Content(string(content)).
			Build()).
		Build()
	_, err := c.client.Im.Message.Create(ctx, req)
	return err
}

func (c *Channel) markMessageIfNew(messageID string, now time.Time) bool {
	if c.dedupeTTL <= 0 {
		c.dedupeTTL = 2 * time.Minute
	}

	if _, loaded := c.processedMessages.LoadOrStore(messageID, now); loaded {
		// Already seen this message (within or past TTL); do not reprocess.
		c.pruneProcessedMessages(now)
		return false
	}

	c.pruneProcessedMessages(now)
	return true
}

func (c *Channel) pruneProcessedMessages(now time.Time) {
	interval := c.dedupeTTL
	if interval <= 0 {
		interval = 2 * time.Minute
	}

	c.pruneMu.Lock()
	if !c.lastPruneAt.IsZero() && now.Sub(c.lastPruneAt) < interval {
		c.pruneMu.Unlock()
		return
	}
	c.lastPruneAt = now
	c.pruneMu.Unlock()

	cutoff := now.Add(-interval)
	c.processedMessages.Range(func(key, value any) bool {
		firstSeen, ok := value.(time.Time)
		if !ok || firstSeen.Before(cutoff) {
			c.processedMessages.Delete(key)
		}
		return true
	})
}

// parseMessageContent extracts text content from a Lark message.
func (c *Channel) parseMessageContent(msg *larkim.EventMessage) string {
	if msg == nil || msg.Content == nil {
		return ""
	}

	content := larkcore.StringValue(msg.Content)
	msgType := "text"
	if msg.MessageType != nil {
		msgType = larkcore.StringValue(msg.MessageType)
	}

	// Parse based on message type
	switch msgType {
	case "text":
		// Text message: {"text": "content"}
		var textMsg struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textMsg); err == nil {
			return textMsg.Text
		}
	case "post":
		// Post/rich text message - extract plain text
		return c.extractPostContent(content)
	case "interactive":
		// Card message - extract action value
		return c.extractInteractiveContent(content)
	}

	// Fallback: return raw content
	return content
}

// extractPostContent extracts plain text from a post message.
func (c *Channel) extractPostContent(content string) string {
	var postMsg struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(content), &postMsg); err != nil {
		return content
	}

	// Parse the rich text content
	var richContent [][]struct {
		Tag     string `json:"tag"`
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(postMsg.Content), &richContent); err != nil {
		return postMsg.Content
	}

	var text strings.Builder
	for _, paragraph := range richContent {
		for _, element := range paragraph {
			if element.Text != "" {
				text.WriteString(element.Text)
			} else if element.Content != "" {
				text.WriteString(element.Content)
			}
		}
		text.WriteString("\n")
	}

	return strings.TrimSpace(text.String())
}

// extractInteractiveContent extracts content from interactive card messages.
func (c *Channel) extractInteractiveContent(content string) string {
	var cardMsg struct {
		Action struct {
			Value map[string]string `json:"value"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(content), &cardMsg); err == nil {
		if v, ok := cardMsg.Action.Value["text"]; ok {
			return v
		}
	}
	return content
}

// sendErrorMessage sends an error message to a chat.
func (c *Channel) sendErrorMessage(ctx context.Context, chatID, errMsg string) error {
	content, _ := json.Marshal(map[string]string{
		"text": fmt.Sprintf("❌ Error: %s", errMsg),
	})

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			ReceiveId(chatID).
			Content(string(content)).
			Build()).
		Build()

	_, err := c.client.Im.Message.Create(ctx, req)
	return err
}

func truncForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// getOrCreateSession returns the session ID for a chat.
// The sessionID is based on chatID, so all messages in the same chat share the same session.
func (c *Channel) getOrCreateSession(chatID, chatType string) string {
	key := chatID
	if v, ok := c.sessions.Load(key); ok {
		return v.(*sessionState).sessionID
	}

	state := &sessionState{
		sessionID: chatID, // Use chat ID as session ID for persistent history
		chatType:  chatType,
	}
	c.sessions.Store(key, state)
	return state.sessionID
}

func (c *Channel) setSession(chatID, sessionID string) {
	key := strings.TrimSpace(chatID)
	if key == "" {
		return
	}
	state := &sessionState{sessionID: strings.TrimSpace(sessionID), chatType: ""}
	if v, ok := c.sessions.Load(key); ok {
		if existing, ok := v.(*sessionState); ok {
			state.chatType = existing.chatType
		}
	}
	if state.sessionID == "" {
		state.sessionID = key
	}
	c.sessions.Store(key, state)
}

// SendToSession implements channel.SessionNotifier. It sends a text message to the
// given session (chat ID), e.g. to deliver cron job output to the Feishu chat that created the job.
func (c *Channel) SendToSession(ctx context.Context, sessionID, text string) error {
	if sessionID == "" || strings.TrimSpace(text) == "" {
		c.printf("lark SendToSession: skip session_id=%q text_empty=%v", sessionID, text == "" || strings.TrimSpace(text) == "")
		return nil
	}
	c.printf("lark SendToSession: session_id=%s len=%d", sessionID, len(text))
	content, _ := json.Marshal(map[string]string{"text": strings.TrimSpace(text)})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			ReceiveId(sessionID).
			Content(string(content)).
			Build()).
		Build()
	_, err := c.client.Im.Message.Create(ctx, req)
	if err != nil {
		c.printf("lark SendToSession: Create message failed session_id=%s err=%v", sessionID, err)
		return err
	}
	c.printf("lark SendToSession: ok session_id=%s", sessionID)
	return nil
}

func (c *Channel) printf(format string, args ...any) {
	if c != nil && c.logf != nil {
		c.logf(format, args...)
	}
}

func (c *Channel) nowTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}
