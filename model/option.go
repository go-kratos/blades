package model

import (
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// Option is the sealed interface for provider hint options.
type Option interface {
	option()
}

// CacheScope defines the scope of a cache hint.
type CacheScope string

const (
	CacheScopeSystem  CacheScope = "system"
	CacheScopeMessage CacheScope = "message"
	CacheScopeTool    CacheScope = "tool"
)

// CacheHint instructs the provider to cache content at a given scope.
type CacheHint struct {
	Scope CacheScope
	TTL   time.Duration
}

func (CacheHint) option() {}

// ReasoningEffort controls the model's reasoning depth.
type ReasoningEffort struct {
	Level string // "minimal", "low", "medium", "high"
}

func (ReasoningEffort) option() {}

// ResponseFormat constrains the model's output format.
type ResponseFormat struct {
	Schema *jsonschema.Schema
	Strict bool
}

func (ResponseFormat) option() {}

// Sampling controls generation parameters.
type Sampling struct {
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	Stop        []string
}

func (Sampling) option() {}

// ParallelToolCalls asks providers to enable or disable model-emitted parallel tool calls.
// Agent loops do not inspect this option; they execute the tool wave returned by the model.
type ParallelToolCalls struct {
	Enabled bool
}

func (ParallelToolCalls) option() {}

// MergeOptions merges request-level options over defaults.
// Request options take precedence by type.
func MergeOptions(defaults, request []Option) []Option {
	if len(request) == 0 {
		return defaults
	}
	if len(defaults) == 0 {
		return request
	}
	seen := make(map[string]struct{})
	result := make([]Option, 0, len(defaults)+len(request))
	for _, o := range request {
		key := optionKey(o)
		seen[key] = struct{}{}
		result = append(result, o)
	}
	for _, o := range defaults {
		key := optionKey(o)
		if _, ok := seen[key]; !ok {
			result = append(result, o)
		}
	}
	return result
}

func optionKey(o Option) string {
	switch v := o.(type) {
	case CacheHint:
		return "cache_hint:" + string(v.Scope)
	case ReasoningEffort:
		return "reasoning_effort"
	case ResponseFormat:
		return "response_format"
	case Sampling:
		return "sampling"
	case ParallelToolCalls:
		return "parallel_tool_calls"
	default:
		return "unknown"
	}
}
