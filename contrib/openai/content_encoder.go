package openai

import (
	"errors"
	"fmt"

	"github.com/go-kratos/blades/content"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ErrUnsupportedContentPart identifies content that no built-in or registered
// provider encoder can translate.
var ErrUnsupportedContentPart = errors.New("openai: unsupported content part")

// UnsupportedContentPartError describes a part that cannot be represented by
// the selected OpenAI API.
type UnsupportedContentPartError struct {
	API  string
	Kind content.Kind
	Type string
}

func (e *UnsupportedContentPartError) Error() string {
	return fmt.Sprintf("openai/%s: unsupported content part %q (%s)", e.API, e.Kind, e.Type)
}

func (e *UnsupportedContentPartError) Unwrap() error { return ErrUnsupportedContentPart }

// ChatContentPartEncoder extends Chat Completions input encoding. Encoders are
// tried in registration order before the built-in encoders. A false handled
// result delegates the part to the next encoder.
type ChatContentPartEncoder interface {
	EncodeChatContentPart(content.Part) (part openaisdk.ChatCompletionContentPartUnionParam, handled bool, err error)
}

// ChatContentPartEncoderFunc adapts a function to ChatContentPartEncoder.
type ChatContentPartEncoderFunc func(content.Part) (openaisdk.ChatCompletionContentPartUnionParam, bool, error)

// EncodeChatContentPart implements ChatContentPartEncoder.
func (f ChatContentPartEncoderFunc) EncodeChatContentPart(part content.Part) (openaisdk.ChatCompletionContentPartUnionParam, bool, error) {
	if f == nil {
		return openaisdk.ChatCompletionContentPartUnionParam{}, false, nil
	}
	return f(part)
}

// ResponsesContentPartEncoder extends Responses API input encoding. Encoders
// are tried in registration order before the built-in encoders. A false
// handled result delegates the part to the next encoder.
type ResponsesContentPartEncoder interface {
	EncodeResponsesContentPart(content.Part) (part responses.ResponseInputContentUnionParam, handled bool, err error)
}

// ResponsesContentPartEncoderFunc adapts a function to ResponsesContentPartEncoder.
type ResponsesContentPartEncoderFunc func(content.Part) (responses.ResponseInputContentUnionParam, bool, error)

// EncodeResponsesContentPart implements ResponsesContentPartEncoder.
func (f ResponsesContentPartEncoderFunc) EncodeResponsesContentPart(part content.Part) (responses.ResponseInputContentUnionParam, bool, error) {
	if f == nil {
		return responses.ResponseInputContentUnionParam{}, false, nil
	}
	return f(part)
}

func unsupportedContentPart(api string, part content.Part) error {
	return &UnsupportedContentPartError{API: api, Kind: contentPartKind(part), Type: fmt.Sprintf("%T", part)}
}

func contentPartKind(part content.Part) content.Kind {
	if part != nil {
		return part.ContentKind()
	}
	return ""
}
