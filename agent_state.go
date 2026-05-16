package blades

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
)

type turnState struct {
	parts      []content.Part
	stopReason event.StopReason
	usage      event.Usage
	action     event.Action
}

func newTurnState() turnState {
	return turnState{stopReason: event.StopEnd}
}

func (s *turnState) recordResponse(resp *model.Response) {
	s.usage.InputTokens += resp.Usage.InputTokens
	s.usage.OutputTokens += resp.Usage.OutputTokens
	if resp.Message != nil {
		s.parts = resp.Message.Parts
	}
}

func (s *turnState) abort() {
	s.stopReason = event.StopAbort
}

func (s *turnState) finish(reason model.StopReason) {
	s.stopReason = outputStopReason(reason)
}

func (s *turnState) stopForAction(action event.Action) {
	s.stopReason = event.StopToolUse
	s.action = action
}

func outputStopReason(reason model.StopReason) event.StopReason {
	if reason == "" {
		return event.StopEnd
	}
	return event.StopReason(reason)
}
