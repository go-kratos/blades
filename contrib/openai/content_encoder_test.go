package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

type videoURLPart struct {
	URL string
	FPS float64
}

func (videoURLPart) ContentKind() content.Kind { return "example.video_url" }

func TestChatContentPartEncoderPreservesCustomPartOrder(t *testing.T) {
	t.Parallel()

	provider := NewChat("compatible-model", WithContentPartEncoders(ChatContentPartEncoderFunc(
		func(part content.Part) (openaisdk.ChatCompletionContentPartUnionParam, bool, error) {
			video, ok := part.(videoURLPart)
			if !ok {
				return openaisdk.ChatCompletionContentPartUnionParam{}, false, nil
			}
			raw, err := json.Marshal(map[string]any{
				"type": "video_url",
				"video_url": map[string]any{
					"url": video.URL,
					"fps": video.FPS,
				},
			})
			if err != nil {
				return openaisdk.ChatCompletionContentPartUnionParam{}, true, err
			}
			return param.Override[openaisdk.ChatCompletionContentPartUnionParam](json.RawMessage(raw)), true, nil
		},
	))).(*chatModel)

	params, err := provider.toChatCompletionParams(false, &model.Request{Messages: []*model.Message{{
		Role: model.RoleUser,
		Parts: []content.Part{
			content.Text{Text: "before"},
			videoURLPart{URL: "https://example.com/video.mp4", FPS: 2},
			content.Text{Text: "after"},
		},
	}}})
	if err != nil {
		t.Fatalf("toChatCompletionParams returned error: %v", err)
	}
	payload, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}

	before := bytes.Index(payload, []byte(`"text":"before"`))
	video := bytes.Index(payload, []byte(`"type":"video_url"`))
	after := bytes.Index(payload, []byte(`"text":"after"`))
	if before < 0 || video <= before || after <= video {
		t.Fatalf("custom content order was not preserved: %s", payload)
	}
	for _, want := range [][]byte{
		[]byte(`"url":"https://example.com/video.mp4"`),
		[]byte(`"fps":2`),
	} {
		if !bytes.Contains(payload, want) {
			t.Fatalf("payload missing %s: %s", want, payload)
		}
	}
}

func TestChatRejectsUnencodedCustomPart(t *testing.T) {
	t.Parallel()

	provider := NewChat("gpt-test").(*chatModel)
	_, err := provider.toChatCompletionParams(false, &model.Request{Messages: []*model.Message{{
		Role:  model.RoleUser,
		Parts: []content.Part{videoURLPart{URL: "https://example.com/video.mp4"}},
	}}})
	if !errors.Is(err, ErrUnsupportedContentPart) {
		t.Fatalf("error = %v, want ErrUnsupportedContentPart", err)
	}
	var unsupported *UnsupportedContentPartError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error type = %T, want *UnsupportedContentPartError", err)
	}
	if got, want := unsupported.Kind, content.Kind("example.video_url"); got != want {
		t.Fatalf("unsupported kind = %q, want %q", got, want)
	}
}

func TestResponsesContentPartEncoderHandlesCustomPart(t *testing.T) {
	t.Parallel()

	provider := NewResponses("compatible-model", WithResponsesContentPartEncoders(ResponsesContentPartEncoderFunc(
		func(part content.Part) (responses.ResponseInputContentUnionParam, bool, error) {
			video, ok := part.(videoURLPart)
			if !ok {
				return responses.ResponseInputContentUnionParam{}, false, nil
			}
			return responses.ResponseInputContentParamOfInputText("video:" + video.URL), true, nil
		},
	))).(*responseModel)

	params, err := provider.toResponseParams(false, &model.Request{Messages: []*model.Message{{
		Role:  model.RoleUser,
		Parts: []content.Part{videoURLPart{URL: "https://example.com/video.mp4"}},
	}}})
	if err != nil {
		t.Fatalf("toResponseParams returned error: %v", err)
	}
	payload, err := json.Marshal(params.Input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"text":"video:https://example.com/video.mp4"`)) {
		t.Fatalf("custom Responses content missing: %s", payload)
	}
}
