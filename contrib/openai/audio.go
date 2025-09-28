package openai

import (
	"context"
	"errors"
	"io"
	"strconv"
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

const defaultAudioVoice = "alloy"

// AudioProvider calls OpenAI's speech synthesis endpoint.
type AudioProvider struct {
	client openai.Client
}

// NewAudioProvider creates a new instance of AudioProvider.
func NewAudioProvider(opts ...option.RequestOption) blades.ModelProvider {
	return &AudioProvider{client: openai.NewClient(opts...)}
}

// Generate generates audio from text input using the configured OpenAI model.
func (p *AudioProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	if req == nil {
		return nil, ErrAudioRequestNil
	}
	if req.Model == "" {
		return nil, ErrAudioModelRequired
	}

	modelOpts := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&modelOpts)
	}

	input, err := promptFromMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	voice := strings.TrimSpace(modelOpts.Audio.Voice)
	if voice == "" {
		voice = defaultAudioVoice
	}

	params := openai.AudioSpeechNewParams{
		Input: input,
		Model: openai.SpeechModel(req.Model),
		Voice: openai.AudioSpeechNewParamsVoice(voice),
	}
	applyAudioOptions(&params, modelOpts.Audio)

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

	mimeType := audioMimeFromContentType(resp.Header.Get("Content-Type"), params.ResponseFormat)
	name := "audio"
	if format := strings.TrimSpace(string(params.ResponseFormat)); format != "" {
		name = "audio." + strings.ToLower(format)
	}

	message := &blades.Message{
		Role:     blades.RoleAssistant,
		Status:   blades.StatusCompleted,
		Metadata: map[string]string{},
		Parts: []blades.Part{
			blades.DataPart{
				Name:     name,
				Bytes:    data,
				MimeType: mimeType,
			},
		},
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		message.Metadata["content_type"] = ct
	}
	if voice != "" {
		message.Metadata["voice"] = voice
	}
	if format := strings.TrimSpace(string(params.ResponseFormat)); format != "" {
		message.Metadata["response_format"] = format
	}
	if modelOpts.Audio.Speed > 0 {
		message.Metadata["speed"] = strconv.FormatFloat(modelOpts.Audio.Speed, 'f', 2, 64)
	}
	if modelOpts.Audio.Instructions != "" {
		message.Metadata["instructions"] = modelOpts.Audio.Instructions
	}

	return &blades.ModelResponse{Messages: []*blades.Message{message}}, nil
}

// NewStream wraps Generate with a single-yield stream for API compatibility.
func (p *AudioProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamer[*blades.ModelResponse], error) {
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

func applyAudioOptions(params *openai.AudioSpeechNewParams, cfg blades.AudioOptions) {
	if cfg.ResponseFormat != "" {
		params.ResponseFormat = openai.AudioSpeechNewParamsResponseFormat(cfg.ResponseFormat)
	}
	if cfg.StreamFormat != "" {
		params.StreamFormat = openai.AudioSpeechNewParamsStreamFormat(cfg.StreamFormat)
	}
	if cfg.Instructions != "" {
		params.Instructions = param.NewOpt(cfg.Instructions)
	}
	if cfg.Speed > 0 {
		params.Speed = param.NewOpt(cfg.Speed)
	}
}

func audioMimeFromContentType(contentType string, format openai.AudioSpeechNewParamsResponseFormat) blades.MimeType {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(ct, "mpeg"), strings.Contains(ct, "mp3"):
		return blades.MimeAudioMP3
	case strings.Contains(ct, "wav"), strings.Contains(ct, "wave"):
		return blades.MimeAudioWAV
	case strings.Contains(ct, "ogg"):
		return blades.MimeAudioOGG
	case strings.Contains(ct, "opus"):
		return blades.MimeAudioOpus
	case strings.Contains(ct, "aac"):
		return blades.MimeAudioAAC
	case strings.Contains(ct, "flac"):
		return blades.MimeAudioFLAC
	case strings.Contains(ct, "pcm"):
		return blades.MimeAudioPCM
	}

	switch strings.ToLower(string(format)) {
	case "mp3":
		return blades.MimeAudioMP3
	case "wav":
		return blades.MimeAudioWAV
	case "opus":
		return blades.MimeAudioOpus
	case "aac":
		return blades.MimeAudioAAC
	case "flac":
		return blades.MimeAudioFLAC
	case "pcm":
		return blades.MimeAudioPCM
	}

	return blades.MimeAudioMP3
}
