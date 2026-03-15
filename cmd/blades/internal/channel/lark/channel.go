// Package lark implements a Lark (Feishu) channel for the blades agent.
// It uses WebSocket mode for receiving messages and supports streaming output.
package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
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
	processedMessages sync.Map // messageID -> first seen unix nanos
	dedupeTTL         time.Duration
	lastPruneAt       int64

	// command handling
	cmdProc   *command.Processor
	reload    func() error
	stop      func() error

	debug bool // when true, log received messages and watch events
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

// WithReload sets the function called when the user issues /reload.
func WithReload(fn func() error) Option {
	return func(c *Channel) { c.reload = fn }
}

// WithStop sets the function called when the user issues /stop.
func WithStop(fn func() error) Option {
	return func(c *Channel) { c.stop = fn }
}

// New creates a new Lark channel.
func New(opts ...Option) *Channel {
	c := &Channel{
		dedupeTTL: 2 * time.Minute,
		cmdProc:   command.NewProcessor(),
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

	fmt.Println("Lark channel connecting via WebSocket...")
	return c.wsClient.Start(ctx)
}

// handleMessage processes a received message.
func (c *Channel) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1, handler channel.StreamHandler) error {
	message := event.Event.Message
	if message == nil {
		return nil
	}

	messageID := strings.TrimSpace(larkcore.StringValue(message.MessageId))
	if messageID == "" {
		return nil
	}
	if !c.markMessageIfNew(messageID, time.Now()) {
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
		log.Printf("lark: received message chat=%s type=%s session=%s content=%q", chatID, chatType, sessionID, truncForLog(content, 200))
	}

	// Check if this is a command
	if strings.HasPrefix(content, "/") {
		return c.handleCommand(ctx, content, sessionID, chatID)
	}

	// Create a card writer for streaming response
	writer := NewCardWriter(ctx, c.client, messageID, chatID, chatType)
	defer func() {
		if err := writer.Close(); err != nil {
			log.Printf("lark: failed to close writer: %v", err)
		}
	}()

	// Process the message with the handler
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
		SessionID:         sessionID,
		ReloadFunc:        c.reload,
		StopFunc:          c.stop,
		SwitchSessionFunc: func(newSessionID string) error { return nil }, // Not applicable in Lark
		ClearFunc:         func() error { return nil },                      // Not applicable in Lark
		Processor:         c.cmdProc,
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

	nowNanos := now.UnixNano()
	if _, loaded := c.processedMessages.LoadOrStore(messageID, nowNanos); loaded {
		// Already seen this message (within or past TTL); do not reprocess.
		c.pruneProcessedMessages(nowNanos)
		return false
	}

	c.pruneProcessedMessages(nowNanos)
	return true
}

func (c *Channel) pruneProcessedMessages(nowNanos int64) {
	interval := c.dedupeTTL.Nanoseconds()
	if interval <= 0 {
		interval = (2 * time.Minute).Nanoseconds()
	}

	last := atomic.LoadInt64(&c.lastPruneAt)
	if last != 0 && nowNanos-last < interval {
		return
	}
	if !atomic.CompareAndSwapInt64(&c.lastPruneAt, last, nowNanos) {
		return
	}

	cutoff := nowNanos - interval
	c.processedMessages.Range(func(key, value any) bool {
		firstSeen, ok := value.(int64)
		if !ok || firstSeen < cutoff {
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

// SendToSession implements channel.SessionNotifier. It sends a text message to the
// given session (chat ID), e.g. to deliver cron job output to the Feishu chat that created the job.
func (c *Channel) SendToSession(ctx context.Context, sessionID, text string) error {
	if sessionID == "" || strings.TrimSpace(text) == "" {
		log.Printf("lark SendToSession: skip session_id=%q text_empty=%v", sessionID, text == "" || strings.TrimSpace(text) == "")
		return nil
	}
	log.Printf("lark SendToSession: session_id=%s len=%d", sessionID, len(text))
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
		log.Printf("lark SendToSession: Create message failed session_id=%s err=%v", sessionID, err)
		return err
	}
	log.Printf("lark SendToSession: ok session_id=%s", sessionID)
	return nil
}
