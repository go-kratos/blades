package openai

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
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
	RequestOptions    []option.RequestOption
}

// imageModel calls OpenAI's image generation endpoints.
type imageModel struct {
	model  string
	config ImageConfig
	client openai.Client
}

// NewImage creates a new instance of imageModel.
func NewImage(model string, config ImageConfig) blades.ModelProvider {
	opts := config.RequestOptions
	// Set base URL and API key if provided
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	return &imageModel{
		model:  model,
		config: config,
		client: openai.NewClient(config.RequestOptions...),
	}
}

// Name returns the name of the OpenAI image model.
func (m *imageModel) Name() string {
	return m.model
}

// Generate generates images using the configured OpenAI model.
func (m *imageModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
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

// NewStreaming wraps Generate with a single-yield stream for API compatibility.
func (m *imageModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		message, err := m.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(message, nil)
	}
}

func (m *imageModel) buildGenerateParams(req *blades.ModelRequest) (openai.ImageGenerateParams, error) {
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
	return params, nil
}

func toImageResponse(res *openai.ImagesResponse) (*blades.ModelResponse, error) {
	message := &blades.Message{
		Role:     blades.RoleAssistant,
		Status:   blades.StatusCompleted,
		Metadata: map[string]any{},
	}
	message.Metadata["size"] = res.Size
	message.Metadata["quality"] = res.Quality
	message.Metadata["background"] = res.Background
	message.Metadata["output_format"] = res.OutputFormat
	message.Metadata["created"] = res.Created
	mimeType := imageMimeType(res.OutputFormat)
	for i, img := range res.Data {
		name := fmt.Sprintf("image-%d", i+1)
		if img.B64JSON != "" {
			data, err := base64.StdEncoding.DecodeString(img.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("openai/image: decode response: %w", err)
			}
			message.Parts = append(message.Parts, blades.DataPart{
				Name:     name,
				Bytes:    data,
				MIMEType: mimeType,
			})
		}
		if img.URL != "" {
			message.Parts = append(message.Parts, blades.FilePart{
				Name:     name,
				URI:      img.URL,
				MIMEType: mimeType,
			})
		}
		if img.RevisedPrompt != "" {
			key := fmt.Sprintf("%s_revised_prompt_%d", name, i+1)
			message.Metadata[key] = img.RevisedPrompt
		}
	}
	return &blades.ModelResponse{Message: message}, nil
}

func imageMimeType(format openai.ImagesResponseOutputFormat) blades.MIMEType {
	switch format {
	case openai.ImagesResponseOutputFormatJPEG:
		return blades.MIMEImageJPEG
	case openai.ImagesResponseOutputFormatWebP:
		return blades.MIMEImageWEBP
	default:
		return blades.MIMEImagePNG
	}
}
