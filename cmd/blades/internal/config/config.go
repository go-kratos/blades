package config

import (
	"time"
)

// Config is the top-level configuration (config.yaml).
// MCP servers are loaded only from mcp.json files, not from config.
type Config struct {
	Providers []Provider    `yaml:"providers"`
	Exec      ExecConfig    `yaml:"exec"`
	Channels  ChannelConfig `yaml:"channels"`
}

// Provider holds credentials and model list for a single model provider.
type Provider struct {
	Provider string   `yaml:"provider"`
	Models   []string `yaml:"models"`
	APIKey   string   `yaml:"apiKey"`
	BaseURL  string   `yaml:"baseURL"`
}

// ChannelConfig groups all channel integrations.
type ChannelConfig struct {
	Lark LarkConfig `yaml:"lark"`
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

// ExecTimeout returns the exec tool timeout duration.
func (e ExecConfig) ExecTimeout() time.Duration {
	if e.TimeoutSeconds > 0 {
		return time.Duration(e.TimeoutSeconds) * time.Second
	}
	return 60 * time.Second
}
