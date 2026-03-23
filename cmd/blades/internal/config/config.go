package config

import (
	"fmt"
	"strings"
	"time"
)

// Config is the top-level configuration (config.yaml).
type Config struct {
	Providers []Provider    `yaml:"providers"`
	Exec      ExecConfig    `yaml:"exec"`
	Channels  ChannelConfig `yaml:"channels"`
}

// Provider holds credentials and model list for a single model provider.
type Provider struct {
	Name     string   `yaml:"name"`
	Provider string   `yaml:"provider"`
	Models   []string `yaml:"models"`
	APIKey   string   `yaml:"apiKey"`
	BaseURL  string   `yaml:"baseUrl"`
}

// Normalize fills derived defaults and validates the config.
func (c *Config) Normalize() error {
	seenNames := make(map[string]struct{}, len(c.Providers))
	for i := range c.Providers {
		p := &c.Providers[i]
		p.Name = strings.TrimSpace(p.Name)
		p.Provider = strings.TrimSpace(p.Provider)
		p.APIKey = strings.TrimSpace(p.APIKey)
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		for j := range p.Models {
			p.Models[j] = strings.TrimSpace(p.Models[j])
		}

		if p.Provider == "" {
			return fmt.Errorf("config: providers[%d].provider is required", i)
		}
		if !isSupportedProvider(p.Provider) {
			return fmt.Errorf("config: providers[%d].provider %q is unsupported (want anthropic|openai|gemini)", i, p.Provider)
		}
		if p.Name == "" {
			p.Name = p.Provider
		}
		if _, ok := seenNames[p.Name]; ok {
			return fmt.Errorf("config: duplicate provider name %q", p.Name)
		}
		seenNames[p.Name] = struct{}{}
	}
	c.Channels.Weixin.AccountDir = ExpandTilde(strings.TrimSpace(c.Channels.Weixin.AccountDir))
	c.Channels.Weixin.StateDir = ExpandTilde(strings.TrimSpace(c.Channels.Weixin.StateDir))
	c.Channels.Weixin.MediaDir = ExpandTilde(strings.TrimSpace(c.Channels.Weixin.MediaDir))
	return nil
}

func isSupportedProvider(name string) bool {
	switch name {
	case "anthropic", "openai", "gemini":
		return true
	default:
		return false
	}
}

// ChannelConfig groups all channel integrations.
type ChannelConfig struct {
	Lark   LarkConfig   `yaml:"lark"`
	Weixin WeixinConfig `yaml:"weixin"`
}

// ExecConfig holds exec tool settings. When empty, built-in defaults are used.
type ExecConfig struct {
	// TimeoutSeconds is the max time a command may run (0 = 60s).
	TimeoutSeconds int `yaml:"timeoutSeconds"`
	// DenyPatterns are regex patterns for commands to block (appended to built-in).
	DenyPatterns []string `yaml:"denyPatterns"`
	// AllowPatterns, when non-empty, restrict commands to those matching at least one.
	AllowPatterns []string `yaml:"allowPatterns"`
	// RestrictToWorkspace blocks path traversal (../) when true.
	RestrictToWorkspace bool `yaml:"restrictToWorkspace"`
}

// LarkConfig configures the Lark/Feishu channel (WebSocket only).
type LarkConfig struct {
	AppID             string `yaml:"appID"`
	AppSecret         string `yaml:"appSecret"`
	EncryptKey        string `yaml:"encryptKey"`
	VerificationToken string `yaml:"verificationToken"`
	Enabled           bool   `yaml:"enabled"`
	// Debug, when true, logs received messages and other watch events to the logger.
	Debug bool `yaml:"debug"`
}

// WeixinConfig configures the Weixin/iLink polling channel.
type WeixinConfig struct {
	AccountID      string   `yaml:"accountID"`
	BotToken       string   `yaml:"botToken"`
	BaseURL        string   `yaml:"baseURL"`
	RouteTag       string   `yaml:"routeTag"`
	ChannelVersion string   `yaml:"channelVersion"`
	AccountDir     string   `yaml:"accountDir"`
	CDNBaseURL     string   `yaml:"cdnBaseURL"`
	StateDir       string   `yaml:"stateDir"`
	MediaDir       string   `yaml:"mediaDir"`
	AllowFrom      []string `yaml:"allowFrom"`
	Enabled        bool     `yaml:"enabled"`
	Debug          bool     `yaml:"debug"`
}

// ExecTimeout returns the exec tool timeout duration.
func (e ExecConfig) ExecTimeout() time.Duration {
	if e.TimeoutSeconds > 0 {
		return time.Duration(e.TimeoutSeconds) * time.Second
	}
	return 60 * time.Second
}
