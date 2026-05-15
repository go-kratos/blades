package openai

import (
	"context"
	"encoding/base64"
	"fmt"
	"iter"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

// ImageConfig holds configuration options for image generation.
type ImageConfig struct {
	BaseURL           string
	APIKey            string
	Background        string
	Size              string
	Quality           string
	ResponseFormat    string
	OutputFormat      string
	Moderation        string
	Style             string
	User              string
	N                 int64
	PartialImages     int64
	OutputCompression int64
	ExtraFields       map[string]any
	RequestOptions    []option.RequestOption
}

// imageModel calls OpenAI's image generation endpoints.
type imageModel struct {
	model  string
	config ImageConfig
	client openai.Client
}

// NewImage creates a new instance of imageModel.
func NewImage(modelName string, config ImageConfig) model.Provider {
	opts := config.RequestOptions
	// Set base URL and API key if provided
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	return &imageModel{
		model:  modelName,
		config: config,
		client: openai.NewClient(opts...),
	}
}

// Name returns the name of the OpenAI image model.
func (m *imageModel) Name() string {
	return m.model
}

// Generate generates images using the configured OpenAI model.
func (m *imageModel) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	params, err := m.buildGenerateParams(req)
	if err != nil {
		return nil, err
	}
	res, err := m.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, err
	}
	return toImageResponse(res)
}

// Stream wraps Generate with a single-yield stream for API compatibility.
func (m *imageModel) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		message, err := m.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		var parts []content.Part
		if message.Message != nil {
			parts = message.Message.Parts
		}
		yield(&model.Chunk{Parts: parts, StopReason: message.StopReason, Usage: &message.Usage}, nil)
	}
}

func (m *imageModel) buildGenerateParams(req *model.Request) (openai.ImageGenerateParams, error) {
	params := openai.ImageGenerateParams{
		Prompt: promptFromMessages(req.Messages),
		Model:  openai.ImageModel(m.model),
	}
	if m.config.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(m.config.Background)
	}
	if m.config.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(m.config.Size)
	}
	if m.config.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(m.config.Quality)
	}
	if m.config.ResponseFormat != "" {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormat(m.config.ResponseFormat)
	}
	if m.config.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(m.config.OutputFormat)
	}
	if m.config.Moderation != "" {
		params.Moderation = openai.ImageGenerateParamsModeration(m.config.Moderation)
	}
	if m.config.Style != "" {
		params.Style = openai.ImageGenerateParamsStyle(m.config.Style)
	}
	if m.config.User != "" {
		params.User = param.NewOpt(m.config.User)
	}
	if m.config.N > 0 {
		params.N = param.NewOpt(m.config.N)
	}
	if m.config.PartialImages > 0 {
		params.PartialImages = param.NewOpt(m.config.PartialImages)
	}
	if m.config.OutputCompression > 0 {
		params.OutputCompression = param.NewOpt(m.config.OutputCompression)
	}
	if len(m.config.ExtraFields) > 0 {
		params.SetExtraFields(m.config.ExtraFields)
	}
	return params, nil
}

func toImageResponse(res *openai.ImagesResponse) (*model.Response, error) {
	message := &model.Message{Role: model.RoleAssistant}
	mimeType := imageMimeType(res.OutputFormat)
	for i, img := range res.Data {
		name := fmt.Sprintf("image-%d", i+1)
		if img.B64JSON != "" {
			data, err := base64.StdEncoding.DecodeString(img.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("openai/image: decode response: %w", err)
			}
			message.Parts = append(message.Parts, content.DataPart{
				Filename: name,
				Bytes:    data,
				MIME:     mimeType,
			})
		}
		if img.URL != "" {
			message.Parts = append(message.Parts, content.FilePart{
				Filename: name,
				URI:      img.URL,
				MIME:     mimeType,
			})
		}
	}
	return &model.Response{Message: message, StopReason: model.StopEnd}, nil
}

func imageMimeType(format openai.ImagesResponseOutputFormat) string {
	switch format {
	case openai.ImagesResponseOutputFormatJPEG:
		return "image/jpeg"
	case openai.ImagesResponseOutputFormatWebP:
		return "image/webp"
	default:
		return "image/png"
	}
}
