package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/go-kratos/blades"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
)

var (
	// ErrPromptRequired is returned when no prompt is provided.
	ErrPromptRequired = errors.New("openai: text prompt is required")
	// ErrImageGenerationEmpty is returned when no images are generated.
	ErrImageGenerationEmpty = errors.New("openai/image: provider returned no images")
)

// ImageOption defines functional options for configuring the ImageProvider.
type ImageOption func(*ImageOptions)

// WithImageOptions applies OpenAI image request options.
func WithImageOptions(opts ...option.RequestOption) ImageOption {
	return func(o *ImageOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// ImageOptions holds configuration for the ImageProvider.
type ImageOptions struct {
	RequestOpts []option.RequestOption
}

// ImageProvider calls OpenAI's image generation endpoints.
type ImageProvider struct {
	opts   ImageOptions
	client openai.Client
}

// NewImageProvider creates a new instance of ImageProvider.
func NewImageProvider(opts ...ImageOption) blades.ModelProvider {
	imageOpts := ImageOptions{}
	for _, opt := range opts {
		opt(&imageOpts)
	}
	return &ImageProvider{
		opts:   imageOpts,
		client: openai.NewClient(imageOpts.RequestOpts...),
	}
}

// Generate generates images using the configured OpenAI model.
func (p *ImageProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
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
		Model:  openai.ImageModel(req.Model),
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

// NewStream wraps Generate with a single-yield stream for API compatibility.
func (p *ImageProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamable[*blades.ModelResponse], error) {
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	pipe.Go(func() error {
		res, err := p.Generate(ctx, req, opts...)
		if err != nil {
			return err
		}
		pipe.Send(res)
		return nil
	})
	return pipe, nil
}

// applyOptions applies image generation options to the OpenAI parameters.
func (p *ImageProvider) applyOptions(params *openai.ImageGenerateParams, opts blades.ImageOptions) error {
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
	if res == nil || len(res.Data) == 0 {
		return nil, ErrImageGenerationEmpty
	}
	message := &blades.Message{
		Role:     blades.RoleAssistant,
		Status:   blades.StatusCompleted,
		Metadata: map[string]string{},
	}
	message.Metadata["size"] = string(res.Size)
	message.Metadata["quality"] = string(res.Quality)
	message.Metadata["background"] = string(res.Background)
	message.Metadata["output_format"] = string(res.OutputFormat)
	message.Metadata["created"] = strconv.FormatInt(res.Created, 10)
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
			key := fmt.Sprintf("%s_revised_prompt_%d", name)
			message.Metadata[key] = img.RevisedPrompt
		}
	}
	if len(message.Parts) == 0 {
		return nil, ErrImageGenerationEmpty
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
