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

// WithAudioVoice sets the voice for the generated audio.
func WithAudioVoice(voice string) AudioOption {
	return func(o *AudioOptions) {
		o.Voice = voice
	}
}

// WithAudioResponseFormat sets the response format of the generated audio.
func WithAudioResponseFormat(format string) AudioOption {
	return func(o *AudioOptions) {
		o.ResponseFormat = format
	}
}

// WithAudioStreamFormat sets the stream format of the generated audio.
func WithAudioStreamFormat(format string) AudioOption {
	return func(o *AudioOptions) {
		o.StreamFormat = format
	}
}

// WithAudioSpeed sets the speed of the generated audio.
func WithAudioSpeed(speed float64) AudioOption {
	return func(o *AudioOptions) {
		o.Speed = &speed
	}
}

// WithAudioOptions appends request options to the audio generation request.
func WithAudioOptions(opts ...option.RequestOption) AudioOption {
	return func(o *AudioOptions) {
		o.RequestOpts = append(o.RequestOpts, opts...)
	}
}

// AudioOptions holds configuration for the audioModel.
type AudioOptions struct {
	Voice          string
	ResponseFormat string
	StreamFormat   string
	Speed          *float64
	RequestOpts    []option.RequestOption
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
func (m *audioModel) Name() string {
	return m.model
}

func (m *audioModel) buildAudioParams(req *blades.ModelRequest) openai.AudioSpeechNewParams {
	params := openai.AudioSpeechNewParams{
		Input: promptFromMessages(req.Messages),
		Model: openai.SpeechModel(m.model),
		Voice: openai.AudioSpeechNewParamsVoice(m.opts.Voice),
	}
	if req.Instruction != nil {
		params.Instructions = param.NewOpt(req.Instruction.Text())
	}
	if m.opts.ResponseFormat != "" {
		params.ResponseFormat = openai.AudioSpeechNewParamsResponseFormat(m.opts.ResponseFormat)
	}
	if m.opts.StreamFormat != "" {
		params.StreamFormat = openai.AudioSpeechNewParamsStreamFormat(m.opts.StreamFormat)
	}
	if m.opts.Speed != nil {
		params.Speed = param.NewOpt(*m.opts.Speed)
	}
	return params
}

// Generate generates audio from text input using the configured OpenAI model.
func (p *audioModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	params := p.buildAudioParams(req)
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
func (p *audioModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		m, err := p.Generate(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(m, nil)
	}
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
