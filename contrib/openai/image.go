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

// WithImageBackground sets the background for the image generation request.
func WithImageBackground(background string) ImageOption {
	return func(o *ImageOptions) {
		o.Background = background
	}
}

// WithImageSize sets the size for the image generation request.
func WithImageSize(size string) ImageOption {
	return func(o *ImageOptions) {
		o.Size = size
	}
}

// WithImageQuality sets the quality for the image generation request.
func WithImageQuality(quality string) ImageOption {
	return func(o *ImageOptions) {
		o.Quality = quality
	}
}

// WithImageResponseFormat sets the response format for the image generation request.
func WithImageResponseFormat(format string) ImageOption {
	return func(o *ImageOptions) {
		o.ResponseFormat = format
	}
}

// WithImageOutputFormat sets the output format for the image generation request.
func WithImageOutputFormat(format string) ImageOption {
	return func(o *ImageOptions) {
		o.OutputFormat = format
	}
}

// WithImageModeration sets the moderation level for the image generation request.
func WithImageModeration(moderation string) ImageOption {
	return func(o *ImageOptions) {
		o.Moderation = moderation
	}
}

// WithImageStyle sets the style for the image generation request.
func WithImageStyle(style string) ImageOption {
	return func(o *ImageOptions) {
		o.Style = style
	}
}

// WithImageUser sets the user identifier for the image generation request.
func WithImageUser(user string) ImageOption {
	return func(o *ImageOptions) {
		o.User = user
	}
}

// WithImageN sets the number of images to generate.
func WithImageN(n int64) ImageOption {
	return func(o *ImageOptions) {
		o.N = n
	}
}

// WithImagePartialImages sets the number of partial images to generate.
func WithImagePartialImages(partialImages int64) ImageOption {
	return func(o *ImageOptions) {
		o.PartialImages = partialImages
	}
}

// WithImageOutputCompression sets the output compression level for generated images.
func WithImageOutputCompression(outputCompression int64) ImageOption {
	return func(o *ImageOptions) {
		o.OutputCompression = outputCompression
	}
}

// WithImageOptions applies OpenAI image request options.
func WithImageOptions(opts ...option.RequestOption) ImageOption {
	return func(o *ImageOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// ImageOptions holds configuration for the imageModel.
type ImageOptions struct {
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
	RequestOpts       []option.RequestOption
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
func (p *imageModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	params, err := p.buildGenerateParams(req)
	if err != nil {
		return nil, err
	}
	res, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, err
	}
	return toImageResponse(res)
}

// NewStreaming wraps Generate with a single-yield stream for API compatibility.
func (p *imageModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		m, err := p.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(m, nil)
	}
}

func (p *imageModel) buildGenerateParams(req *blades.ModelRequest) (openai.ImageGenerateParams, error) {
	params := openai.ImageGenerateParams{
		Prompt: promptFromMessages(req.Messages),
		Model:  openai.ImageModel(p.model),
	}
	if p.opts.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(p.opts.Background)
	}
	if p.opts.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(p.opts.Size)
	}
	if p.opts.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(p.opts.Quality)
	}
	if p.opts.ResponseFormat != "" {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormat(p.opts.ResponseFormat)
	}
	if p.opts.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(p.opts.OutputFormat)
	}
	if p.opts.Moderation != "" {
		params.Moderation = openai.ImageGenerateParamsModeration(p.opts.Moderation)
	}
	if p.opts.Style != "" {
		params.Style = openai.ImageGenerateParamsStyle(p.opts.Style)
	}
	if p.opts.User != "" {
		params.User = param.NewOpt(p.opts.User)
	}
	if p.opts.N > 0 {
		params.N = param.NewOpt(int64(p.opts.N))
	}
	if p.opts.PartialImages > 0 {
		params.PartialImages = param.NewOpt(int64(p.opts.PartialImages))
	}
	if p.opts.OutputCompression > 0 {
		params.OutputCompression = param.NewOpt(p.opts.OutputCompression)
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
