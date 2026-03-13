package config

import "gopkg.in/yaml.v3"

// Config is the top-level workspace configuration.
type Config struct {
	// Workspace is the root directory path. Defaults to ~/.blades.
	Workspace string `yaml:"workspace"`

	// LLM configures the language model provider.
	LLM LLMConfig `yaml:"llm"`

	// Defaults holds agent execution parameters.
	Defaults DefaultsConfig `yaml:"defaults"`

	// MCP lists MCP server connections whose tools are exposed to the agent.
	MCP []MCPServerConfig `yaml:"mcp"`
}

// MCPServerConfig configures a single MCP server connection.
type MCPServerConfig struct {
	// Name is a unique identifier for this server.
	Name string `yaml:"name"`
	// Transport is one of: stdio, http, websocket.
	Transport string `yaml:"transport"`
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
	// Headers are custom HTTP headers (http transport).
	Headers map[string]string `yaml:"headers"`
	// TimeoutSeconds is the request timeout (0 → default 30 s).
	TimeoutSeconds int `yaml:"timeoutSeconds"`
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
