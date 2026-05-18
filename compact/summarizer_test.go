package compact

import (
	"context"
	"iter"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSummarizerUsesDirectProviderRequest(t *testing.T) {
	provider := &recordingSummaryProvider{
		resp: &model.Response{
			Message: &model.Message{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: " summary text "}}},
		},
	}

	summary, err := NewModelSummarizer(provider, WithSummaryInstruction("summary instruction")).Summarize(context.Background(), SummaryRequest{
		Messages:  []*model.Message{textMessage(model.RoleUser, "old user")},
		MaxTokens: 42,
	})
	require.NoError(t, err)

	assert.Equal(t, "summary text", summary)
	require.NotNil(t, provider.req)
	assert.Equal(t, "summary-provider", provider.req.Model)
	assert.Equal(t, "summary instruction", provider.req.System)
	assert.Empty(t, provider.req.Tools)
	require.Len(t, provider.req.Messages, 1)
	assert.Contains(t, content.TextFromParts(provider.req.Messages[0].Parts), "old user")
	assertSummaryMaxTokens(t, provider.req.Options, 42)
}

func assertSummaryMaxTokens(t *testing.T, opts []model.Option, want int) {
	t.Helper()
	for _, opt := range opts {
		sampling, ok := opt.(model.Sampling)
		if !ok {
			continue
		}
		if assert.NotNil(t, sampling.MaxTokens) {
			assert.Equal(t, want, *sampling.MaxTokens)
		}
		return
	}
	t.Fatalf("summary request missing Sampling option")
}

type recordingSummaryProvider struct {
	req  *model.Request
	resp *model.Response
}

func (p *recordingSummaryProvider) Name() string {
	return "summary-provider"
}

func (p *recordingSummaryProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	p.req = req
	return p.resp, nil
}

func (p *recordingSummaryProvider) Stream(context.Context, *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(func(*model.Chunk, error) bool) {}
}

func TestFormatSummaryResponseStripsAnalysisExtractsSummary(t *testing.T) {
	input := "<analysis>\nthinking about stuff\n</analysis>\n\n<summary>\nThe user asked for X.\n</summary>"
	got := formatSummaryResponse(input)
	assert.Equal(t, "The user asked for X.", got)
}

func TestFormatSummaryResponseStripsAnalysisOnly(t *testing.T) {
	input := "<analysis>\nthinking\n</analysis>\n\nRemaining text here."
	got := formatSummaryResponse(input)
	assert.Equal(t, "Remaining text here.", got)
}

func TestFormatSummaryResponsePassthroughWithoutTags(t *testing.T) {
	input := "Plain summary without any tags."
	got := formatSummaryResponse(input)
	assert.Equal(t, "Plain summary without any tags.", got)
}
