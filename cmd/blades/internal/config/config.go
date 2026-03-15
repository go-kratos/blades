package config

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration (config.yaml).
// MCP servers are loaded only from mcp.json files, not from config.
type Config struct {
	LLM      LLMConfig      `yaml:"llm"`
	Defaults DefaultsConfig `yaml:"defaults"`
	Exec     ExecConfig     `yaml:"exec"`
	Lark     LarkConfig     `yaml:"lark"`
}

// LLMConfig specifies the provider and model connection details.
type LLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"apiKey"`
	BaseURL  string `yaml:"baseURL"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that both "provider" and "name"
// are accepted for the provider field, and both "baseURL" and "baseUrl" for the base URL field.
func (c *LLMConfig) UnmarshalYAML(value *yaml.Node) error {
	type raw struct {
		Provider string `yaml:"provider"`
		Name     string `yaml:"name"`
		Model    string `yaml:"model"`
		APIKey   string `yaml:"apiKey"`
		BaseURL  string `yaml:"baseURL"`
		BaseUrl  string `yaml:"baseUrl"` //nolint:revive // intentional alias
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	c.Provider = r.Provider
	if c.Provider == "" {
		c.Provider = r.Name
	}
	c.Model = r.Model
	c.APIKey = r.APIKey
	c.BaseURL = r.BaseURL
	if c.BaseURL == "" {
		c.BaseURL = r.BaseUrl
	}
	return nil
}

// DefaultsConfig holds agent execution defaults.
type DefaultsConfig struct {
	MaxIterations     int  `yaml:"maxIterations"`
	MaxTurns          int  `yaml:"maxTurns"`
	MemoryWindow      int  `yaml:"memoryWindow"`
	CompressThreshold int  `yaml:"compressThreshold"`
	LogConversation   bool `yaml:"logConversation"`
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
	AppID              string `yaml:"appID"`
	AppSecret          string `yaml:"appSecret"`
	EncryptKey         string `yaml:"encryptKey"`
	VerificationToken string `yaml:"verificationToken"`
	Enabled            bool   `yaml:"enabled"`
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
