package gemini

import (
	"context"
	"fmt"
	"iter"

	"github.com/go-kratos/blades/model"
	"google.golang.org/genai"
)

// Config holds configuration for the Gemini model.
type Config struct {
	genai.ClientConfig
	Seed             int32
	MaxOutputTokens  int32
	Temperature      float32
	TopP             float32
	TopK             float32
	PresencePenalty  float32
	FrequencyPenalty float32
	StopSequences    []string
	ThinkingConfig   *genai.ThinkingConfig
	ModelOptions     []model.Option
}

// ModelOption configures a Gemini model provider.
type ModelOption func(*Config)

// WithConfig applies a full Config value.
func WithConfig(config Config) ModelOption {
	return func(c *Config) {
		*c = config
	}
}

// WithClientConfig applies the GenAI SDK client configuration.
func WithClientConfig(config genai.ClientConfig) ModelOption {
	return func(c *Config) {
		c.ClientConfig = config
	}
}

// NewModel creates a Gemini model provider.
func NewModel(ctx context.Context, modelName string, opts ...ModelOption) (model.Provider, error) {
	var config Config
	for _, opt := range opts {
		opt(&config)
	}
	client, err := genai.NewClient(ctx, &config.ClientConfig)
	if err != nil {
		return nil, err
	}
	return &Gemini{
		model:  modelName,
		config: config,
		client: client,
	}, nil
}

// Gemini provides a unified interface for Gemini API access.
type Gemini struct {
	model  string
	config Config
	client *genai.Client
}

var _ model.Provider = (*Gemini)(nil)

// Name returns the name of the model.
func (m *Gemini) Name() string {
	return m.model
}

// Generate executes a non-streaming generation request.
func (m *Gemini) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	system, contents, err := convertMessageToGenAI(req)
	if err != nil {
		return nil, err
	}
	config, err := m.toGenerateConfig(req)
	if err != nil {
		return nil, err
	}
	config.SystemInstruction = system
	resp, err := m.client.Models.GenerateContent(ctx, m.model, contents, config)
	if err != nil {
		return nil, err
	}
	return convertGenAIToBlades(resp)
}

// Stream executes a streaming generation request.
func (m *Gemini) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		system, contents, err := convertMessageToGenAI(req)
		if err != nil {
			yield(nil, err)
			return
		}
		config, err := m.toGenerateConfig(req)
		if err != nil {
			yield(nil, err)
			return
		}
		config.SystemInstruction = system
		for resp, err := range m.client.Models.GenerateContentStream(ctx, m.model, contents, config) {
			if err != nil {
				yield(nil, err)
				return
			}
			chunk, err := convertGenAIToChunk(resp)
			if err != nil {
				yield(nil, err)
				return
			}
			if len(chunk.Parts) == 0 && chunk.StopReason == "" && chunk.Usage == nil {
				continue
			}
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (m *Gemini) toGenerateConfig(req *model.Request) (*genai.GenerateContentConfig, error) {
	var config genai.GenerateContentConfig
	if m.config.Temperature > 0 {
		config.Temperature = &m.config.Temperature
	}
	if m.config.TopP > 0 {
		config.TopP = &m.config.TopP
	}
	if m.config.TopK > 0 {
		config.TopK = &m.config.TopK
	}
	if m.config.MaxOutputTokens > 0 {
		config.MaxOutputTokens = m.config.MaxOutputTokens
	}
	if len(m.config.StopSequences) > 0 {
		config.StopSequences = m.config.StopSequences
	}
	if m.config.PresencePenalty > 0 {
		config.PresencePenalty = &m.config.PresencePenalty
	}
	if m.config.FrequencyPenalty > 0 {
		config.FrequencyPenalty = &m.config.FrequencyPenalty
	}
	if m.config.Seed > 0 {
		config.Seed = &m.config.Seed
	}
	if m.config.ThinkingConfig != nil {
		config.ThinkingConfig = m.config.ThinkingConfig
	}
	if req != nil && len(req.Tools) > 0 {
		tools, err := convertBladesToolsToGenAI(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("converting tools: %w", err)
		}
		config.Tools = tools
	}
	if req != nil {
		applyModelOptions(&config, model.MergeOptions(m.config.ModelOptions, req.Options))
	} else {
		applyModelOptions(&config, m.config.ModelOptions)
	}
	return &config, nil
}

func applyModelOptions(config *genai.GenerateContentConfig, opts []model.Option) {
	for _, opt := range opts {
		switch o := opt.(type) {
		case model.Sampling:
			if o.Temperature != nil {
				v := float32(*o.Temperature)
				config.Temperature = &v
			}
			if o.TopP != nil {
				v := float32(*o.TopP)
				config.TopP = &v
			}
			if o.MaxTokens != nil {
				config.MaxOutputTokens = int32(*o.MaxTokens)
			}
			if len(o.Stop) > 0 {
				config.StopSequences = o.Stop
			}
		}
	}
}
