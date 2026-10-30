// Package minimax provides first-class helpers that adapt MiniMax models to
// the generic blades.ModelProvider interface.
//
// MiniMax exposes both an OpenAI-compatible chat completion endpoint and an
// Anthropic-compatible messages endpoint, served from two regions. NewModel
// builds a provider on the OpenAI-compatible endpoint, while NewAnthropicModel
// builds one on the Anthropic-compatible endpoint. Both default their base URL
// to the selected Region.
package minimax

import (
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/anthropic"
	"github.com/go-kratos/blades/contrib/openai"
)

// Region identifies a MiniMax service region.
type Region string

const (
	// RegionGlobal is the global (international) MiniMax service.
	RegionGlobal Region = "global_en"
	// RegionChina is the mainland China MiniMax service.
	RegionChina Region = "cn_zh"
)

// Model identifiers exposed by this package.
const (
	// ModelM31Flash is the fast, cost-efficient model with a one-million token
	// context window and text, image and video input.
	ModelM31Flash = "MiniMax-M3.1-Flash"
	// ModelM3 is the flagship model with a one-million token context window and
	// text, image and video input.
	ModelM3 = "MiniMax-M3"
)

// DefaultModel is the recommended default text model.
const DefaultModel = ModelM31Flash

// Thinking modes advertised by MiniMax models.
const (
	ThinkingAdaptive = "adaptive"
	ThinkingDisabled = "disabled"
)

// Endpoint holds the per-region base URLs and documentation root.
type Endpoint struct {
	// OpenAIBaseURL is the OpenAI-compatible chat completion base URL.
	OpenAIBaseURL string
	// AnthropicBaseURL is the Anthropic-compatible messages base URL.
	AnthropicBaseURL string
	// DocsRoot is the region documentation root.
	DocsRoot string
}

// Endpoints maps each region to its base URLs.
var Endpoints = map[Region]Endpoint{
	RegionGlobal: {
		OpenAIBaseURL:    "https://api.minimax.io/v1",
		AnthropicBaseURL: "https://api.minimax.io/anthropic",
		DocsRoot:         "https://platform.minimax.io/docs",
	},
	RegionChina: {
		OpenAIBaseURL:    "https://api.minimaxi.com/v1",
		AnthropicBaseURL: "https://api.minimaxi.com/anthropic",
		DocsRoot:         "https://platform.minimaxi.com/docs",
	},
}

// Pricing captures the USD cost per one million tokens.
type Pricing struct {
	Input     float64
	Output    float64
	CacheRead float64
	// CacheWrite is nil when the model does not price cache writes separately.
	CacheWrite *float64
}

// PricingTier associates token pricing with the largest request context for
// which that pricing applies.
type PricingTier struct {
	MaxContextTokens int
	Pricing          Pricing
}

// ModelInfo describes a MiniMax text model.
type ModelInfo struct {
	// ContextWindow is the maximum number of tokens the model accepts.
	ContextWindow int
	// PricingTiers contains the USD cost per one million tokens, ordered by
	// increasing context size.
	PricingTiers []PricingTier
	// InputModalities lists the accepted input modalities.
	InputModalities []string
	// Thinking lists the supported thinking modes.
	Thinking []string
}

// Models describes the text models exposed by this package.
var Models = map[string]ModelInfo{
	ModelM31Flash: {
		ContextWindow: 1000000,
		PricingTiers: []PricingTier{
			{MaxContextTokens: 1000000, Pricing: Pricing{Input: 0.15, Output: 0.6, CacheRead: 0.015}},
		},
		InputModalities: []string{"text", "image", "video"},
		Thinking:        []string{ThinkingAdaptive},
	},
	ModelM3: {
		ContextWindow: 1000000,
		PricingTiers: []PricingTier{
			{MaxContextTokens: 512000, Pricing: Pricing{Input: 0.3, Output: 1.2, CacheRead: 0.06}},
			{MaxContextTokens: 1000000, Pricing: Pricing{Input: 0.6, Output: 2.4, CacheRead: 0.12}},
		},
		InputModalities: []string{"text", "image", "video"},
		Thinking:        []string{ThinkingAdaptive, ThinkingDisabled},
	},
}

// Config configures a MiniMax provider. Region selects the service region and
// defaults to RegionGlobal. BaseURL, when set, overrides the region base URL.
// The remaining fields are forwarded to the underlying transport.
type Config struct {
	// Region selects the MiniMax service region. Defaults to RegionGlobal.
	Region Region
	// APIKey is the MiniMax API key. When empty the underlying client falls
	// back to its own environment-variable lookup.
	APIKey string
	// BaseURL overrides the region base URL when non-empty.
	BaseURL string
	// MaxOutputTokens caps the number of generated tokens when greater than 0.
	MaxOutputTokens int64
	// Temperature is the sampling temperature applied when greater than 0.
	Temperature float64
	// TopP is the nucleus sampling probability applied when greater than 0.
	TopP float64
	// StopSequences are sequences that stop generation.
	StopSequences []string
}

// resolveRegion returns a known region, defaulting to RegionGlobal.
func resolveRegion(r Region) Region {
	if _, ok := Endpoints[r]; ok {
		return r
	}
	return RegionGlobal
}

// openAIBaseURL resolves the OpenAI-compatible base URL for the config.
func (c Config) openAIBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return Endpoints[resolveRegion(c.Region)].OpenAIBaseURL
}

// anthropicBaseURL resolves the Anthropic-compatible base URL for the config.
func (c Config) anthropicBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return Endpoints[resolveRegion(c.Region)].AnthropicBaseURL
}

// NewModel builds a MiniMax provider on the OpenAI-compatible chat endpoint.
func NewModel(model string, config Config) blades.ModelProvider {
	return openai.NewModel(model, openai.Config{
		BaseURL:         config.openAIBaseURL(),
		APIKey:          config.APIKey,
		MaxOutputTokens: config.MaxOutputTokens,
		Temperature:     config.Temperature,
		TopP:            config.TopP,
		StopSequences:   config.StopSequences,
	})
}

// NewAnthropicModel builds a MiniMax provider on the Anthropic-compatible
// messages endpoint.
func NewAnthropicModel(model string, config Config) blades.ModelProvider {
	return anthropic.NewModel(model, anthropic.Config{
		BaseURL:         config.anthropicBaseURL(),
		APIKey:          config.APIKey,
		MaxOutputTokens: config.MaxOutputTokens,
		Temperature:     config.Temperature,
		TopP:            config.TopP,
		StopSequences:   config.StopSequences,
	})
}
