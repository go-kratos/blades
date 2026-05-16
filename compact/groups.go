package compact

import (
	"fmt"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

type messageGroup struct {
	start int
	end   int
}

func messageGroups(msgs []*model.Message) ([]messageGroup, error) {
	groups := make([]messageGroup, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg == nil {
			groups = append(groups, messageGroup{start: i, end: i + 1})
			continue
		}
		switch msg.Role {
		case model.RoleTool:
			return nil, fmt.Errorf("compact: dangling tool message at index %d", i)
		case model.RoleAssistant:
			uses := toolUseIDs(msg.Parts)
			if len(uses) == 0 {
				groups = append(groups, messageGroup{start: i, end: i + 1})
				continue
			}
			if i+1 >= len(msgs) || msgs[i+1] == nil || msgs[i+1].Role != model.RoleTool {
				return nil, fmt.Errorf("compact: assistant tool use at index %d has no following tool result", i)
			}
			if err := validateToolResults(i+1, uses, msgs[i+1].Parts); err != nil {
				return nil, err
			}
			groups = append(groups, messageGroup{start: i, end: i + 2})
			i++
		default:
			groups = append(groups, messageGroup{start: i, end: i + 1})
		}
	}
	return groups, nil
}

func toolUseIDs(parts []content.Part) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, part := range parts {
		if toolUse, ok := part.(content.ToolUse); ok {
			ids[toolUse.ID] = struct{}{}
		}
	}
	return ids
}

func validateToolResults(index int, uses map[string]struct{}, parts []content.Part) error {
	results := make(map[string]struct{})
	for _, part := range parts {
		result, ok := part.(content.ToolResult)
		if !ok {
			continue
		}
		if _, ok := uses[result.ID]; !ok {
			return fmt.Errorf("compact: tool result %q at index %d has no matching tool use", result.ID, index)
		}
		results[result.ID] = struct{}{}
	}
	for id := range uses {
		if _, ok := results[id]; !ok {
			return fmt.Errorf("compact: tool use %q has no matching tool result at index %d", id, index)
		}
	}
	return nil
}

func retainLastMessages(groups []messageGroup, maxMessages int) int {
	if len(groups) == 0 {
		return 0
	}
	if maxMessages <= 0 {
		return groups[len(groups)-1].start
	}
	total := 0
	start := groups[len(groups)-1].start
	for i := len(groups) - 1; i >= 0; i-- {
		size := groups[i].end - groups[i].start
		if total > 0 && total+size > maxMessages {
			break
		}
		total += size
		start = groups[i].start
	}
	return start
}

func isGroupBoundary(groups []messageGroup, index int) bool {
	if index == 0 {
		return true
	}
	for _, group := range groups {
		if group.start == index || group.end == index {
			return true
		}
	}
	return false
}
