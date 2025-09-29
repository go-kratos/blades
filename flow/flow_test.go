package flow

import (
    "context"
    "testing"

    "github.com/go-kratos/blades"
)

type jsonRunner struct { seen *blades.Prompt }

func (r *jsonRunner) Run(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (*blades.Generation, error) {
    r.seen = blades.NewPrompt(prompt.Messages...)
    return &blades.Generation{Messages: []*blades.Message{blades.AssistantMessage(`{"ok":true}`)}}, nil
}

func (r *jsonRunner) RunStream(ctx context.Context, prompt *blades.Prompt, opts ...blades.ModelOption) (blades.Streamer[*blades.Generation], error) {
    g, err := r.Run(ctx, prompt, opts...)
    if err != nil { return nil, err }
    p := blades.NewStreamPipe[*blades.Generation]()
    p.Send(g)
    p.Close()
    return p, nil
}

func TestFlow_DefaultTemplates(t *testing.T) {
    r := &jsonRunner{}
    type out struct{ Ok bool `json:"ok"` }
    f := NewFlow[string, out](r).
        WithSystemTemplate("S: {{.}}").
        WithUserTemplate("U: {{.}}")
    got, err := f.Run(context.Background(), "X")
    if err != nil { t.Fatalf("Run error: %v", err) }
    if !got.Ok { t.Fatalf("unexpected output: %+v", got) }
    if r.seen == nil || len(r.seen.Messages) != 2 {
        t.Fatalf("expected 2 messages in prompt, got %d", len(r.seen.Messages))
    }
    if r.seen.Messages[0].AsText() != "S: X" || r.seen.Messages[1].AsText() != "U: X" {
        t.Fatalf("unexpected prompt content: %#v | %#v", r.seen.Messages[0].AsText(), r.seen.Messages[1].AsText())
    }
}

func TestFlow_CustomBuilder(t *testing.T) {
    r := &jsonRunner{}
    type out struct{ Ok bool `json:"ok"` }
    f := NewFlow[string, out](r).
        WithSystemTemplate("ignored").
        WithUserTemplate("ignored").
        WithPromptBuilder(func(s string) (*blades.Prompt, error) {
            return blades.NewPrompt(blades.SystemMessage("CUSTOM:"+s)), nil
        })
    _, err := f.Run(context.Background(), "Y")
    if err != nil { t.Fatalf("Run error: %v", err) }
    if r.seen == nil || len(r.seen.Messages) != 1 || r.seen.Messages[0].AsText() != "CUSTOM:Y" {
        t.Fatalf("builder not applied; got: %#v", r.seen)
    }
}
