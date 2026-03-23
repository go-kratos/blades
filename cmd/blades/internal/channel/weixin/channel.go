package weixin

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	wx "github.com/daemon365/weixin-clawbot"

	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/command"
)

const channelName = "weixin"

// Channel implements channel.Channel for the Weixin/iLink long-polling API.
type Channel struct {
	account        wx.Account
	routeTag       string
	channelVersion string
	cdnBaseURL     string
	syncBufPath    string
	mediaDir       string
	allowFrom      []string
	httpClient     *http.Client

	cmdProc      *command.Processor
	stop         func() error
	clearSession func(string) error

	sessions sync.Map // userID -> sessionID

	debug  bool
	output io.Writer
	logf   func(string, ...any)
}

// Option configures a Channel.
type Option func(*Channel)

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

// WithHTTPClient overrides the HTTP client used for iLink requests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Channel) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// New creates a new Weixin channel.
func New(account wx.Account, syncBufPath string, opts ...Option) *Channel {
	c := &Channel{
		account:     account,
		syncBufPath: strings.TrimSpace(syncBufPath),
		httpClient:  &http.Client{},
		cmdProc:     command.NewProcessor(),
		output:      io.Discard,
		logf:        log.Printf,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.output == nil {
		c.output = io.Discard
	}
	if c.logf == nil {
		c.logf = log.Printf
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	return c
}

// Name implements channel.Channel.
func (c *Channel) Name() string { return channelName }

// Start implements channel.Channel.
func (c *Channel) Start(ctx context.Context, handler channel.StreamHandler) error {
	if handler == nil {
		return fmt.Errorf("stream handler is nil")
	}

	fmt.Fprintln(c.output, "Weixin channel connecting via long polling...")
	api := wx.NewAPIClient(wx.APIOptions{
		BaseURL:        c.account.BaseURL,
		Token:          c.account.BotToken,
		RouteTag:       c.routeTag,
		ChannelVersion: c.channelVersion,
		HTTPClient:     c.httpClient,
		AccountID:      c.account.AccountID,
	})
	return wx.Monitor(ctx, wx.MonitorOptions{
		API:         api,
		AccountID:   c.account.AccountID,
		SyncBufPath: c.syncBufPath,
		AllowFrom:   c.allowFrom,
		OnMessages: func(ctx context.Context, messages []wx.WeixinMessage) error {
			return c.handleMessages(ctx, messages, handler)
		},
		OnError: func(err error) {
			c.printf("weixin: monitor error: %v", err)
		},
	})
}

func (c *Channel) handleMessages(ctx context.Context, messages []wx.WeixinMessage, handler channel.StreamHandler) error {
	for _, msg := range messages {
		if err := c.handleMessage(ctx, msg, handler); err != nil {
			userID := strings.TrimSpace(msg.FromUserID)
			if userID == "" {
				c.printf("weixin: handle message failed err=%v", err)
				continue
			}
			c.printf("weixin: handle message failed user=%s err=%v", userID, err)
		}
	}
	return nil
}

func (c *Channel) handleMessage(ctx context.Context, msg wx.WeixinMessage, handler channel.StreamHandler) error {
	if msg.MessageType == wx.MessageTypeBot {
		return nil
	}
	userID := strings.TrimSpace(msg.FromUserID)
	if userID == "" {
		return nil
	}
	if token := strings.TrimSpace(msg.ContextToken); token != "" {
		wx.SetContextToken(c.account.AccountID, userID, token)
	}

	content, mediaPath, err := c.buildInboundText(ctx, msg)
	if err != nil {
		c.printf("weixin: build inbound text user=%s err=%v", userID, err)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		if mediaPath == "" {
			if hasMediaItem(msg.ItemList) {
				content = "[received media]"
			} else {
				return nil
			}
		}
		if mediaPath != "" {
			content = "[received media] " + filepath.Base(mediaPath)
		}
	}

	sessionID := c.getOrCreateSession(userID)
	if c.debug {
		c.printf("weixin: received message user=%s session=%s len=%d", userID, sessionID, len(content))
	}

	if strings.HasPrefix(content, "/") {
		return c.handleCommand(ctx, content, sessionID, userID)
	}

	reply, err := handler(ctx, sessionID, content, discardWriter{})
	if err != nil {
		sendErr := c.sendText(ctx, userID, "Error: "+err.Error())
		if sendErr != nil {
			c.printf("weixin: failed to send error to=%s err=%v", userID, sendErr)
		}
		return err
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	return c.sendText(ctx, userID, reply)
}

func (c *Channel) buildInboundText(ctx context.Context, msg wx.WeixinMessage) (string, string, error) {
	text := strings.TrimSpace(wx.BodyFromItemList(msg.ItemList))
	if c.cdnBaseURL == "" || c.mediaDir == "" {
		return text, "", nil
	}
	for _, item := range msg.ItemList {
		if !wx.IsMediaItem(item) {
			continue
		}
		media, err := wx.DownloadMediaFromItem(ctx, item, c.cdnBaseURL, c.httpClient, wx.SaveMediaToDir(c.mediaDir), nil)
		if err != nil {
			return text, "", err
		}
		if media == nil {
			return text, "", nil
		}
		switch {
		case media.DecryptedPicPath != "":
			return text, media.DecryptedPicPath, nil
		case media.DecryptedVideoPath != "":
			return text, media.DecryptedVideoPath, nil
		case media.DecryptedFilePath != "":
			return text, media.DecryptedFilePath, nil
		case media.DecryptedVoicePath != "":
			return text, media.DecryptedVoicePath, nil
		}
		return text, "", nil
	}
	return text, "", nil
}

func (c *Channel) handleCommand(ctx context.Context, line, sessionID, userID string) error {
	env := &command.Environment{
		SessionID: sessionID,
		StopFunc:  c.stop,
		SwitchSessionFunc: func(newSessionID string) error {
			c.setSession(userID, newSessionID)
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
		return c.sendText(ctx, userID, "Command error: "+err.Error())
	}
	if result == nil {
		return nil
	}
	msg := result.Message
	if result.IsError {
		msg = "Error: " + msg
	}
	return c.sendText(ctx, userID, msg)
}

func (c *Channel) getOrCreateSession(userID string) string {
	key := strings.TrimSpace(userID)
	if key == "" {
		return ""
	}
	if v, ok := c.sessions.Load(key); ok {
		if sessionID, ok := v.(string); ok && strings.TrimSpace(sessionID) != "" {
			return sessionID
		}
	}
	c.sessions.Store(key, key)
	return key
}

func (c *Channel) setSession(userID, sessionID string) {
	key := strings.TrimSpace(userID)
	if key == "" {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = key
	}
	c.sessions.Store(key, sessionID)
}

// SendToSession implements channel.SessionNotifier.
func (c *Channel) SendToSession(ctx context.Context, sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	return c.sendText(ctx, c.resolveUserID(sessionID), text)
}

func (c *Channel) sendText(ctx context.Context, toUserID, text string) error {
	toUserID = strings.TrimSpace(toUserID)
	text = strings.TrimSpace(text)
	if toUserID == "" || text == "" {
		return nil
	}
	contextToken := strings.TrimSpace(wx.GetContextToken(c.account.AccountID, toUserID))
	if contextToken == "" {
		return fmt.Errorf("weixin context token missing for %s", toUserID)
	}
	_, err := wx.SendMessageWeixin(ctx, toUserID, wx.MarkdownToPlainText(text), wx.SendOptions{
		BaseURL:        c.account.BaseURL,
		Token:          c.account.BotToken,
		RouteTag:       c.routeTag,
		ChannelVersion: c.channelVersion,
		HTTPClient:     c.httpClient,
		ContextToken:   contextToken,
		AccountID:      c.account.AccountID,
	})
	return err
}

func (c *Channel) resolveUserID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if token := strings.TrimSpace(wx.GetContextToken(c.account.AccountID, sessionID)); token != "" {
		return sessionID
	}
	resolved := sessionID
	c.sessions.Range(func(key, value any) bool {
		userID, ok := key.(string)
		if !ok {
			return true
		}
		currentSession, ok := value.(string)
		if !ok || strings.TrimSpace(currentSession) != sessionID {
			return true
		}
		resolved = userID
		return false
	})
	return resolved
}

func (c *Channel) printf(format string, args ...any) {
	if c != nil && c.logf != nil {
		c.logf(format, args...)
	}
}

func hasMediaItem(items []wx.MessageItem) bool {
	for _, item := range items {
		if wx.IsMediaItem(item) {
			return true
		}
	}
	return false
}

type discardWriter struct{}

func (discardWriter) WriteText(string) {}

func (discardWriter) WriteEvent(channel.Event) {}
