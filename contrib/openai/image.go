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

// ImageProvider calls OpenAI's image generation endpoints.
type ImageProvider struct {
	client openai.Client
}

// NewImageProvider creates a new instance of ImageProvider.
func NewImageProvider(opts ...option.RequestOption) blades.ModelProvider {
	return &ImageProvider{client: openai.NewClient(opts...)}
}

// Generate generates images using the configured OpenAI model.
func (p *ImageProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	if req == nil {
		return nil, errors.New("openai/image: request is nil")
	}
	modelOpts := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&modelOpts)
	}
	prompt, err := promptFromMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	params := openai.ImageGenerateParams{Prompt: prompt}
	if req.Model != "" {
		params.Model = openai.ImageModel(req.Model)
	}
	applyImageOptions(&params, req.Model, modelOpts.Image)
	res, err := p.client.Images.Generate(ctx, params)
	if err != nil {
		return nil, err
	}
	return toImageResponse(res)
}

// NewStream wraps Generate with a single-yield stream for API compatibility.
func (p *ImageProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamer[*blades.ModelResponse], error) {
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

func applyImageOptions(params *openai.ImageGenerateParams, model string, cfg blades.ImageOptions) {
	if cfg.Background != "" {
		params.Background = openai.ImageGenerateParamsBackground(cfg.Background)
	}
	if cfg.Size != "" {
		params.Size = openai.ImageGenerateParamsSize(cfg.Size)
	}
	if cfg.Quality != "" {
		params.Quality = openai.ImageGenerateParamsQuality(cfg.Quality)
	}
	if cfg.ResponseFormat != "" {
		params.ResponseFormat = openai.ImageGenerateParamsResponseFormat(cfg.ResponseFormat)
	}
	if cfg.OutputFormat != "" {
		params.OutputFormat = openai.ImageGenerateParamsOutputFormat(cfg.OutputFormat)
	}
	if cfg.Moderation != "" {
		params.Moderation = openai.ImageGenerateParamsModeration(cfg.Moderation)
	}
	if cfg.Style != "" {
		params.Style = openai.ImageGenerateParamsStyle(cfg.Style)
	}
	if cfg.User != "" {
		params.User = param.NewOpt(cfg.User)
	}
	if cfg.Count > 0 {
		params.N = param.NewOpt(int64(cfg.Count))
	}
	if cfg.PartialImages > 0 {
		params.PartialImages = param.NewOpt(int64(cfg.PartialImages))
	}
	if cfg.OutputCompression > 0 {
		params.OutputCompression = param.NewOpt(int64(cfg.OutputCompression))
	}
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
	if res.OutputFormat != "" {
		message.Metadata["output_format"] = string(res.OutputFormat)
	}
	if res.Quality != "" {
		message.Metadata["quality"] = string(res.Quality)
	}
	if res.Size != "" {
		message.Metadata["size"] = string(res.Size)
	}
	if res.Background != "" {
		message.Metadata["background"] = string(res.Background)
	}
	if res.Created != 0 {
		message.Metadata["created"] = strconv.FormatInt(res.Created, 10)
	}
	mimeType := mimeFromOutputFormat(res.OutputFormat)
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
				MimeType: mimeType,
			})
		}
		if img.URL != "" {
			message.Parts = append(message.Parts, blades.FilePart{
				Name:     name,
				URI:      img.URL,
				MimeType: mimeType,
			})
		}
		if img.RevisedPrompt != "" {
			key := fmt.Sprintf("%s_revised_prompt", name)
			message.Metadata[key] = img.RevisedPrompt
		}
	}
	if len(message.Parts) == 0 {
		return nil, ErrImageGenerationEmpty
	}
	return &blades.ModelResponse{Messages: []*blades.Message{message}}, nil
}

func mimeFromOutputFormat(format openai.ImagesResponseOutputFormat) blades.MimeType {
	switch format {
	case openai.ImagesResponseOutputFormatJPEG:
		return blades.MimeImageJPEG
	case openai.ImagesResponseOutputFormatWebP:
		return blades.MimeImageWEBP
	default:
		return blades.MimeImagePNG
	}
}
