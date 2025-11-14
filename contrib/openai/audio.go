package openai

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/go-kratos/blades"
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

var _ blades.ModelProvider = (*audioModel)(nil)

// AudioOption defines functional options for configuring the audioModel.
type AudioOption func(*AudioOptions)

// WithAudioOptions appends request options to the audio generation request.
func WithAudioOptions(opts ...option.RequestOption) AudioOption {
	return func(o *AudioOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// AudioOptions holds configuration for the audioModel.
type AudioOptions struct {
	RequestOpts []option.RequestOption
}

// audioModel calls OpenAI's speech synthesis endpoint.
type audioModel struct {
	model  string
	opts   AudioOptions
	client openai.Client
}

// NewAudio creates a new instance of audioModel.
func NewAudio(model string, opts ...AudioOption) blades.ModelProvider {
	audioOpts := AudioOptions{}
	for _, opt := range opts {
		opt(&audioOpts)
	}
	return &audioModel{
		opts:   audioOpts,
		client: openai.NewClient(audioOpts.RequestOpts...),
	}
}

// Name returns the name of the audio model.
func (p *audioModel) Name() string {
	return p.model
}

// Generate generates audio from text input using the configured OpenAI model.
func (p *audioModel) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
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
		Model: openai.SpeechModel(p.model),
		Voice: openai.AudioSpeechNewParamsVoice(modelOpts.Audio.Voice),
	}
	if req.Instruction != nil {
		params.Instructions = param.NewOpt(req.Instruction.Text())
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

// NewStreaming wraps Generate with a single-yield stream for API compatibility.
func (p *audioModel) NewStreaming(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		m, err := p.Generate(ctx, req, opts...)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(m, nil)
	}
}

func (p *audioModel) applyOptions(params *openai.AudioSpeechNewParams, opt blades.AudioOptions) error {
	if opt.ResponseFormat != "" {
		params.ResponseFormat = openai.AudioSpeechNewParamsResponseFormat(opt.ResponseFormat)
	}
	if opt.StreamFormat != "" {
		params.StreamFormat = openai.AudioSpeechNewParamsStreamFormat(opt.StreamFormat)
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
