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

// ImageOption defines functional options for configuring the imageModel.
type ImageOption func(*ImageOptions)

// WithImageOptions applies OpenAI image request options.
func WithImageOptions(opts ...option.RequestOption) ImageOption {
	return func(o *ImageOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// ImageOptions holds configuration for the imageModel.
type ImageOptions struct {
	RequestOpts []option.RequestOption
}

// imageModel calls OpenAI's image generation endpoints.
type imageModel struct {
	model  string
	opts   ImageOptions
	client openai.Client
}

// NewImage creates a new instance of imageModel.
func NewImage(model string, opts ...ImageOption) blades.ModelProvider {
	imageOpts := ImageOptions{}
	for _, opt := range opts {
		opt(&imageOpts)
	}
	return &imageModel{
		model:  model,
		opts:   imageOpts,
		client: openai.NewClient(imageOpts.RequestOpts...),
	}
}

// Name returns the name of the OpenAI image model.
func (p *imageModel) Name() string {
	return p.model
}

// Generate generates images using the configured OpenAI model.
func (p *imageModel) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	modelOpts := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&modelOpts)
	}
	prompt, err := promptFromMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	params := openai.ImageGenerateParams{
		Prompt: prompt,
		Model:  openai.ImageModel(p.model),
	}
	if err := p.applyOptions(&params, modelOpts.Image); err != nil {
		return nil, err
	}
	res, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, err
	}
	return toImageResponse(res)
}

// NewStreaming wraps Generate with a single-yield stream for API compatibility.
func (p *imageModel) NewStreaming(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		m, err := p.Generate(ctx, req, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(m, nil)
	}
}

// applyOptions applies image generation options to the OpenAI parameters.
func (p *imageModel) applyOptions(params *openai.ImageGenerateParams, opts blades.ImageOptions) error {
	if opts.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(opts.Background)
	}
	if opts.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(opts.Size)
	}
	if opts.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(opts.Quality)
	}
	if opts.ResponseFormat != "" {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormat(opts.ResponseFormat)
	}
	if opts.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(opts.OutputFormat)
	}
	if opts.Moderation != "" {
		params.Moderation = openai.ImageGenerateParamsModeration(opts.Moderation)
	}
	if opts.Style != "" {
		params.Style = openai.ImageGenerateParamsStyle(opts.Style)
	}
	if opts.User != "" {
		params.User = param.NewOpt(opts.User)
	}
	if opts.Count > 0 {
		params.N = param.NewOpt(int64(opts.Count))
	}
	if opts.PartialImages > 0 {
		params.PartialImages = param.NewOpt(int64(opts.PartialImages))
	}
	if opts.OutputCompression > 0 {
		params.OutputCompression = param.NewOpt(int64(opts.OutputCompression))
	}
	return nil
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
