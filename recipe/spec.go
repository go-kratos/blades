package recipe

// ExecutionMode defines how sub-agents are executed.
type ExecutionMode string

const (
	// ExecutionSequential runs sub-agents one after another.
	ExecutionSequential ExecutionMode = "sequential"
	// ExecutionParallel runs sub-agents concurrently.
	ExecutionParallel ExecutionMode = "parallel"
	// ExecutionTool wraps each sub-agent as a tool for the parent agent.
	ExecutionTool ExecutionMode = "tool"
)

// ParameterType defines the type of a recipe parameter.
type ParameterType string

const (
	ParameterString  ParameterType = "string"
	ParameterNumber  ParameterType = "number"
	ParameterBoolean ParameterType = "boolean"
	ParameterSelect  ParameterType = "select"
)

// ParameterRequirement defines whether a parameter is required or optional.
type ParameterRequirement string

const (
	ParameterRequired ParameterRequirement = "required"
	ParameterOptional ParameterRequirement = "optional"
)

// ContextStrategy defines the context management strategy for an agent.
type ContextStrategy string

const (
	// ContextStrategySummarize compresses old messages using an LLM-based rolling summary.
	ContextStrategySummarize ContextStrategy = "summarize"
	// ContextStrategyWindow truncates oldest messages to stay within a token or message budget.
	ContextStrategyWindow ContextStrategy = "window"
)

// ContextSpec configures the context window management for an agent.
// It maps to either a summarizing or a sliding-window ContextManager.
//
// Example (summarize):
//
//	context:
//	  strategy: summarize
//	  max_tokens: 80000
//	  keep_recent: 10
//	  batch_size: 20
//	  model: gpt-4o-mini
//
// Example (window):
//
//	context:
//	  strategy: window
//	  max_tokens: 80000
//	  max_messages: 100
type ContextSpec struct {
	// Strategy selects the implementation: "summarize" or "window".
	Strategy ContextStrategy `yaml:"strategy"`
	// MaxTokens is the token budget. When exceeded, old messages are compressed or dropped.
	MaxTokens int64 `yaml:"max_tokens,omitempty"`
	// KeepRecent is the number of recent messages always kept verbatim (summarize only, default 10).
	KeepRecent int `yaml:"keep_recent,omitempty"`
	// BatchSize is the number of messages summarized per compression pass (summarize only, default 20).
	BatchSize int `yaml:"batch_size,omitempty"`
	// MaxMessages is the maximum number of messages to retain (window only).
	MaxMessages int `yaml:"max_messages,omitempty"`
	// Model is the model name used for summarization (summarize strategy only).
	// If omitted, falls back to the agent's own model.
	Model string `yaml:"model,omitempty"`
}

// MiddlewareSpec declares a single middleware to apply to an agent.
// The middleware is resolved by name from the MiddlewareRegistry at build time,
// with Options passed as-is to the factory function.
//
// Example:
//
//	middlewares:
//	  - name: tracing
//	  - name: logging
//	    options:
//	      level: info
type MiddlewareSpec struct {
	Name    string         `yaml:"name"`
	Options map[string]any `yaml:"options,omitempty"`
}

// AgentSpec is the top-level declarative specification for a recipe.
// A recipe YAML file is parsed into this structure and then built into a blades.Agent.
type AgentSpec struct {
	Version       string          `yaml:"version"`
	Name          string          `yaml:"name"`
	Description   string          `yaml:"description"`
	Model         string          `yaml:"model,omitempty"`
	Instruction   string          `yaml:"instruction"`
	Prompt        string          `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec `yaml:"parameters,omitempty"`
	SubAgents    []SubAgentSpec  `yaml:"sub_agents,omitempty"`
	Execution     ExecutionMode   `yaml:"execution,omitempty"`
	Tools         []string        `yaml:"tools,omitempty"`
	OutputKey     string          `yaml:"output_key,omitempty"`
	MaxIterations int             `yaml:"max_iterations,omitempty"`
	Context       *ContextSpec    `yaml:"context,omitempty"`
	Middlewares   []MiddlewareSpec `yaml:"middlewares,omitempty"`
}

// SubAgentSpec defines a child agent within a recipe.
type SubAgentSpec struct {
	Name          string          `yaml:"name"`
	Description   string          `yaml:"description,omitempty"`
	Model         string          `yaml:"model,omitempty"`
	Instruction   string          `yaml:"instruction"`
	Prompt        string          `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec `yaml:"parameters,omitempty"`
	Tools         []string        `yaml:"tools,omitempty"`
	OutputKey     string          `yaml:"output_key,omitempty"`
	MaxIterations int             `yaml:"max_iterations,omitempty"`
	Context       *ContextSpec    `yaml:"context,omitempty"`
	Middlewares   []MiddlewareSpec `yaml:"middlewares,omitempty"`
}

// ParameterSpec defines a configurable parameter for a recipe.
type ParameterSpec struct {
	Name        string               `yaml:"name"`
	Type        ParameterType        `yaml:"type"`
	Description string               `yaml:"description"`
	Default     any                  `yaml:"default,omitempty"`
	Required    ParameterRequirement `yaml:"required,omitempty"`
	Options     []string             `yaml:"options,omitempty"`
}
