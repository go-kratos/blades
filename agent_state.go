package blades

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
)

type turnState struct {
	parts      []content.Part
	stopReason event.StopReason
	usage      model.Usage
	action     event.Action
}

func newTurnState() turnState {
	return turnState{stopReason: event.StopEnd}
}

func (s *turnState) recordResponse(resp *model.Response) {
	s.usage = resp.Usage
	s.parts = resp.Message.Parts
	s.stopReason = outputStopReason(resp.StopReason)
}

func (s *turnState) abort() {
	s.stopReason = event.StopAbort
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
