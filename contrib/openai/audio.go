package openai

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
)

var (
	// ErrAudioGenerationEmpty is returned when the provider returns no audio data.
	ErrAudioGenerationEmpty = errors.New("openai/audio: provider returned no audio")
	// ErrAudioRequestNil is returned when the request is nil.
	ErrAudioRequestNil = errors.New("openai/audio: request is nil")
	// ErrAudioModelRequired is returned when the model is not specified.
	ErrAudioModelRequired = errors.New("openai/audio: model is required")
	// ErrAudioVoiceRequired is returned when the voice is not specified.
	ErrAudioVoiceRequired = errors.New("openai/audio: voice is required")
)

// AudioOption defines functional options for configuring the AudioProvider.
type AudioOption func(*AudioOptions)

// WithAudioOptions appends request options to the audio generation request.
func WithAudioOptions(opts ...option.RequestOption) AudioOption {
	return func(o *AudioOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// AudioOptions holds configuration for the AudioProvider.
type AudioOptions struct {
	RequestOpts []option.RequestOption
}

// AudioProvider calls OpenAI's speech synthesis endpoint.
type AudioProvider struct {
	opts   AudioOptions
	client openai.Client
}

// NewAudioProvider creates a new instance of AudioProvider.
func NewAudioProvider(opts ...AudioOption) blades.ModelProvider {
	audioOpts := AudioOptions{}
	for _, opt := range opts {
		opt(&audioOpts)
	}
	return &AudioProvider{
		opts:   audioOpts,
		client: openai.NewClient(audioOpts.RequestOpts...),
	}
}

// Generate generates audio from text input using the configured OpenAI model.
func (p *AudioProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	modelOpts := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&modelOpts)
	}
	input, err := promptFromMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	params := openai.AudioSpeechNewParams{
		Input: input,
		Model: openai.SpeechModel(req.Model),
		Voice: openai.AudioSpeechNewParamsVoice(modelOpts.Audio.Voice),
	}
	if err := p.applyOptions(&params, modelOpts.Audio); err != nil {
		return nil, err
	}
	resp, err := p.client.Audio.Speech.New(ctx, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrAudioGenerationEmpty
	}
	name := "audio." + strings.ToLower(string(params.ResponseFormat))
	mimeType := audioMimeType(params.ResponseFormat)
	message := &blades.Message{
		Role:     blades.RoleAssistant,
		Status:   blades.StatusCompleted,
		Metadata: map[string]any{},
		Parts: []blades.Part{
			blades.DataPart{
				Name:     name,
				Bytes:    data,
				MIMEType: mimeType,
			},
		},
	}
	message.Metadata["content_type"] = resp.Header.Get("Content-Type")
	message.Metadata["response_format"] = params.ResponseFormat
	return &blades.ModelResponse{Message: message}, nil
}

// NewStream wraps Generate with a single-yield stream for API compatibility.
func (p *AudioProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (stream.Streamable[*blades.ModelResponse], error) {
	return stream.Go(func(yield func(*blades.ModelResponse, error) bool) {
		m, err := p.Generate(ctx, req, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(m, nil)
	}), nil
}

func (p *AudioProvider) applyOptions(params *openai.AudioSpeechNewParams, opt blades.AudioOptions) error {
	if opt.ResponseFormat != "" {
		params.ResponseFormat = openai.AudioSpeechNewParamsResponseFormat(opt.ResponseFormat)
	}
	if opt.StreamFormat != "" {
		params.StreamFormat = openai.AudioSpeechNewParamsStreamFormat(opt.StreamFormat)
	}
	if opt.Instructions != "" {
		params.Instructions = param.NewOpt(opt.Instructions)
	}
	if opt.Speed > 0 {
		params.Speed = param.NewOpt(opt.Speed)
	}
	return nil
}

func audioMimeType(format openai.AudioSpeechNewParamsResponseFormat) blades.MIMEType {
	switch strings.ToLower(string(format)) {
	case "mp3":
		return blades.MIMEAudioMP3
	case "wav":
		return blades.MIMEAudioWAV
	case "opus":
		return blades.MIMEAudioOpus
	case "aac":
		return blades.MIMEAudioAAC
	case "flac":
		return blades.MIMEAudioFLAC
	case "pcm":
		return blades.MIMEAudioPCM
	}
	return blades.MIMEAudioMP3
}
