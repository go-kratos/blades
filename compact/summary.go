package compact

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/session"
)

const blockSummaryStateKey = "__compact_block_summary_state__"

// NewBlockSummarize creates a compactor that folds older message groups into
// summary blocks while preserving recent raw messages.
func NewBlockSummarize(opts ...Option) Compactor {
	cfg := options{
		keepRecentMessages:   8,
		maxSummaryBlocks:     4,
		summaryBatchMessages: 20,
		maxFoldIterations:    8,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.maxFoldIterations <= 0 {
		cfg.maxFoldIterations = 1
	}
	return &blockSummarizeCompactor{
		messagesBudget:       cfg.messagesBudget,
		keepRecentMessages:   cfg.keepRecentMessages,
		keepRecentTokens:     cfg.keepRecentTokens,
		summaryBlockTokens:   cfg.summaryBlockTokens,
		maxSummaryBlocks:     cfg.maxSummaryBlocks,
		summaryBatchMessages: cfg.summaryBatchMessages,
		maxFoldIterations:    cfg.maxFoldIterations,
		counter:              cfg.counter,
		summarizer:           cfg.summarizer,
	}
}

type blockSummarizeCompactor struct {
	messagesBudget       int64
	keepRecentMessages   int
	keepRecentTokens     int64
	summaryBlockTokens   int64
	maxSummaryBlocks     int
	summaryBatchMessages int
	maxFoldIterations    int
	counter              model.TokenCounter
	summarizer           Summarizer
}

type blockSummaryState struct {
	Version int                 `json:"version"`
	Blocks  []blockSummaryBlock `json:"blocks"`
}

type blockSummaryBlock struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Summary string `json:"summary"`
}

func (b *blockSummarizeCompactor) Compact(ctx context.Context, req Request) ([]*model.Message, error) {
	msgs := req.Messages
	if len(msgs) == 0 {
		return msgs, nil
	}
	groups, err := messageGroups(msgs)
	if err != nil {
		return nil, err
	}
	sess, _ := session.FromContext(ctx)
	state := b.loadState(sess)
	if !state.validFor(len(msgs), groups) {
		state = blockSummaryState{Version: 1}
	}

	changed := false
	for i := 0; i < b.maxFoldIterations; i++ {
		covered := state.covered()
		recentStart, err := b.recentStart(ctx, req.TokenCounter, msgs, groups, covered)
		if err != nil {
			return nil, err
		}
		view := assembleSummaryView(state.Blocks, msgs)
		shouldFold, err := b.shouldFold(ctx, req.TokenCounter, view, covered, recentStart)
		if err != nil {
			return nil, err
		}
		if !shouldFold {
			break
		}
		if b.summarizer == nil {
			break
		}
		foldEnd := b.foldEnd(groups, covered, recentStart)
		if foldEnd <= covered {
			break
		}
		summary, err := b.summarizer.Summarize(ctx, SummaryRequest{
			Messages:  msgs[covered:foldEnd],
			MaxTokens: b.summaryBlockTokens,
		})
		if err != nil {
			return nil, err
		}
		state.Version = 1
		state.Blocks = append(state.Blocks, blockSummaryBlock{
			Start:   covered,
			End:     foldEnd,
			Summary: summary,
		})
		changed = true
		if err := b.mergeBlocks(ctx, &state); err != nil {
			return nil, err
		}
	}

	if changed && sess != nil {
		sess.SetState(blockSummaryStateKey, state)
	}
	return assembleSummaryView(state.Blocks, msgs), nil
}

func (b *blockSummarizeCompactor) loadState(sess session.Session) blockSummaryState {
	if sess == nil {
		return blockSummaryState{Version: 1}
	}
	value, ok := sess.State()[blockSummaryStateKey]
	if !ok {
		return blockSummaryState{Version: 1}
	}
	switch state := value.(type) {
	case blockSummaryState:
		return state
	case *blockSummaryState:
		if state != nil {
			return *state
		}
	}
	return blockSummaryState{Version: 1}
}

func (s blockSummaryState) validFor(msgLen int, groups []messageGroup) bool {
	lastEnd := 0
	for _, block := range s.Blocks {
		if block.Start != lastEnd || block.End <= block.Start || block.End > msgLen {
			return false
		}
		if !isGroupBoundary(groups, block.Start) || !isGroupBoundary(groups, block.End) {
			return false
		}
		lastEnd = block.End
	}
	return true
}

func (s blockSummaryState) covered() int {
	if len(s.Blocks) == 0 {
		return 0
	}
	return s.Blocks[len(s.Blocks)-1].End
}

func (b *blockSummarizeCompactor) shouldFold(ctx context.Context, reqCounter model.TokenCounter, view []*model.Message, covered int, recentStart int) (bool, error) {
	if covered < recentStart {
		return true, nil
	}
	if b.messagesBudget > 0 {
		counter := b.counter
		if counter == nil {
			counter = reqCounter
		}
		tokens, err := countMessagesTokens(ctx, counter, view...)
		if err != nil {
			return false, err
		}
		return tokens > b.messagesBudget, nil
	}
	return false, nil
}

func (b *blockSummarizeCompactor) recentStart(ctx context.Context, reqCounter model.TokenCounter, msgs []*model.Message, groups []messageGroup, covered int) (int, error) {
	keepMessages := b.keepRecentMessages
	if GetHint(ctx) == HintShrink && keepMessages > 1 {
		keepMessages = keepMessages / 2
		if keepMessages < 1 {
			keepMessages = 1
		}
	}

	recentStart := len(msgs)
	if keepMessages > 0 {
		recentStart = retainLastMessages(groups, keepMessages)
	}
	if b.keepRecentTokens > 0 {
		counter := b.counter
		if counter == nil {
			counter = reqCounter
		}
		for recentStart < len(msgs) {
			tokens, err := countMessagesTokens(ctx, counter, msgs[recentStart:]...)
			if err != nil {
				return 0, err
			}
			if tokens <= b.keepRecentTokens {
				break
			}
			next := nextGroupStart(groups, recentStart)
			if next <= recentStart || next >= len(msgs) {
				break
			}
			recentStart = next
		}
	}
	if recentStart < covered {
		return covered, nil
	}
	return recentStart, nil
}

func nextGroupStart(groups []messageGroup, start int) int {
	for i, group := range groups {
		if group.start == start && i+1 < len(groups) {
			return groups[i+1].start
		}
	}
	return lenFromGroups(groups)
}

func lenFromGroups(groups []messageGroup) int {
	if len(groups) == 0 {
		return 0
	}
	return groups[len(groups)-1].end
}

func (b *blockSummarizeCompactor) foldEnd(groups []messageGroup, covered int, recentStart int) int {
	limit := b.summaryBatchMessages
	total := 0
	foldEnd := covered
	for _, group := range groups {
		if group.end <= covered {
			continue
		}
		if group.end > recentStart {
			break
		}
		size := group.end - group.start
		if limit > 0 && total > 0 && total+size > limit {
			break
		}
		total += size
		foldEnd = group.end
		if limit > 0 && total >= limit {
			break
		}
	}
	return foldEnd
}

func (b *blockSummarizeCompactor) mergeBlocks(ctx context.Context, state *blockSummaryState) error {
	if b.maxSummaryBlocks <= 0 {
		return nil
	}
	for len(state.Blocks) > b.maxSummaryBlocks {
		if b.summarizer == nil {
			state.Blocks = state.Blocks[1:]
			continue
		}
		first := state.Blocks[0]
		second := state.Blocks[1]
		summary, err := b.summarizer.Summarize(ctx, SummaryRequest{
			Messages: []*model.Message{
				summaryMessage(1, first),
				summaryMessage(2, second),
			},
			MaxTokens: b.summaryBlockTokens,
		})
		if err != nil {
			return err
		}
		merged := blockSummaryBlock{
			Start:   first.Start,
			End:     second.End,
			Summary: summary,
		}
		state.Blocks = append([]blockSummaryBlock{merged}, state.Blocks[2:]...)
	}
	return nil
}

func assembleSummaryView(blocks []blockSummaryBlock, msgs []*model.Message) []*model.Message {
	covered := 0
	result := make([]*model.Message, 0, len(blocks)+len(msgs))
	for i, block := range blocks {
		result = append(result, summaryMessage(i+1, block))
		covered = block.End
	}
	result = append(result, msgs[covered:]...)
	return result
}

func summaryMessage(index int, block blockSummaryBlock) *model.Message {
	return &model.Message{
		Role: model.RoleUser,
		Parts: []content.Part{content.Text{Text: fmt.Sprintf(
			"[Compact summary %d: messages %d-%d]\n%s",
			index,
			block.Start,
			block.End,
			block.Summary,
		)}},
	}
}
