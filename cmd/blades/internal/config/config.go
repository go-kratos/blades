package config

import (
	"time"

	bladesmcp "github.com/go-kratos/blades/contrib/mcp"
	"gopkg.in/yaml.v3"
)

// Config is the top-level workspace configuration.
type Config struct {
	// LLM configures the language model provider.
	LLM LLMConfig

	// Defaults holds agent execution parameters.
	Defaults DefaultsConfig

	// MCP lists MCP server connections whose tools are exposed to the agent.
	MCP []bladesmcp.ClientConfig
}

// mcpEntryYAML is a YAML-friendly struct for MCP server entries in config.yaml.
// It supports field aliases (type → transport, url → endpoint) for compatibility
// with various config formats including Claude Desktop's mcp.json style.
type mcpEntryYAML struct {
	// Name is a unique identifier for this server.
	Name string `yaml:"name"`
	// Transport is one of: stdio (default), http, websocket.
	Transport string `yaml:"transport"`
	// Type is an alias for Transport.
	Type string `yaml:"type"`
	// Command is the executable (stdio transport).
	Command string `yaml:"command"`
	// Args are the command arguments (stdio transport).
	Args []string `yaml:"args"`
	// Env contains extra environment variables (stdio transport).
	Env map[string]string `yaml:"env"`
	// WorkDir is the working directory (stdio transport).
	WorkDir string `yaml:"workDir"`
	// Endpoint is the server URL (http / websocket transport).
	Endpoint string `yaml:"endpoint"`
	// URL is an alias for Endpoint.
	URL string `yaml:"url"`
	// Headers are custom HTTP headers (http transport).
	Headers map[string]string `yaml:"headers"`
	// TimeoutSeconds is the request timeout (0 → default 30 s).
	TimeoutSeconds int `yaml:"timeoutSeconds"`
}

func (e mcpEntryYAML) toClientConfig() bladesmcp.ClientConfig {
	transport := e.Transport
	if transport == "" {
		transport = e.Type
	}
	if transport == "" {
		transport = "stdio"
	}
	endpoint := e.Endpoint
	if endpoint == "" {
		endpoint = e.URL
	}
	cc := bladesmcp.ClientConfig{
		Name:      e.Name,
		Transport: bladesmcp.TransportType(transport),
		Command:   e.Command,
		Args:      e.Args,
		Env:       e.Env,
		WorkDir:   e.WorkDir,
		Endpoint:  endpoint,
		Headers:   e.Headers,
	}
	if e.TimeoutSeconds > 0 {
		cc.Timeout = time.Duration(e.TimeoutSeconds) * time.Second
	}
	return cc
}

// rawConfig is used internally for YAML unmarshaling to handle MCP entry aliases.
type rawConfig struct {
	LLM      LLMConfig      `yaml:"llm"`
	Defaults DefaultsConfig `yaml:"defaults"`
	MCP      []mcpEntryYAML `yaml:"mcp"`
}

// UnmarshalYAML implements yaml.Unmarshaler to convert MCP entries to bladesmcp.ClientConfig.
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	var raw rawConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.LLM = raw.LLM
	c.Defaults = raw.Defaults
	for _, e := range raw.MCP {
		c.MCP = append(c.MCP, e.toClientConfig())
	}
	return nil
}

// LLMConfig specifies the provider and model connection details.
type LLMConfig struct {
	// Provider is one of: anthropic, openai, gemini.
	// Also accepted as "name" in YAML for convenience.
	Provider string `yaml:"provider"`

	// Model is the model name, e.g. "claude-sonnet-4-6".
	Model string `yaml:"model"`

	// APIKey is the provider API key. Supports ${ENV_VAR} expansion.
	APIKey string `yaml:"apiKey"`

	// BaseURL overrides the default API endpoint (optional).
	// Also accepted as "baseUrl" in YAML.
	BaseURL string `yaml:"baseURL"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that both "provider" and "name"
// are accepted for the provider field, and both "baseURL" and "baseUrl" for the
// base URL field.
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
	// MaxIterations is the maximum number of tool-call iterations per turn.
	// 0 uses the library default (10).
	MaxIterations int `yaml:"maxIterations"`

	// MaxTurns is the maximum conversation turns per session.
	// 0 means unlimited.
	MaxTurns int `yaml:"maxTurns"`

	// MemoryWindow is retained for backward compatibility with older config files.
	// Startup context is no longer auto-injected; AGENTS.md should instruct the
	// agent which files to read at runtime.
	MemoryWindow int `yaml:"memoryWindow"`

	// CompressThreshold is the session message count that triggers context truncation.
	// 0 disables. Default: 40000 (characters).
	CompressThreshold int `yaml:"compressThreshold"`
}
