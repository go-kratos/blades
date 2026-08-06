package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/skills"
	"github.com/go-kratos/blades/tests/dummyprovider"
	"github.com/go-kratos/blades/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentLoadsSkillIntoNextModelRequest(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "add-friend", Description: "Use when the user asks to add a friend."},
		"Search for candidates first, then ask for confirmation.",
		skills.Resources{},
	)
	require.NoError(t, err)
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"add-friend"}`)),
		dummyprovider.TextResponse("I found the matching friend."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithSkills(skill),
		blades.WithHooks(capture),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("Add Alice as a friend"))
	require.NoError(t, err)
	turns := turnEnds(outputs)
	require.Len(t, turns, 2)
	assert.Equal(t, "I found the matching friend.", turns[1].Text())

	requests := capture.Requests()
	require.Len(t, requests, 2)
	assert.Contains(t, requests[0].System, "<available_skills>")
	assert.Contains(t, requests[0].System, "add-friend")
	assert.NotContains(t, requests[0].System, "Search for candidates")
	assert.ElementsMatch(t, []string{
		skills.ToolListSkillsName,
		skills.ToolLoadSkillName,
		skills.ToolLoadSkillResourceName,
	}, toolNames(requests[0].Tools))

	var loadedInstruction string
	for _, message := range requests[1].Messages {
		if message.Role == model.RoleTool {
			loadedInstruction += toolResultText(message.Parts)
		}
	}
	assert.Contains(t, loadedInstruction, "Search for candidates first")
}

func TestAgentSkillToolsRespectPolicy(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "private-skill", Description: "A protected skill."},
		"private instructions",
		skills.Resources{},
	)
	require.NoError(t, err)
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"private-skill"}`)),
		dummyprovider.TextResponse("I cannot load that skill."),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithSkills(skill),
		blades.WithPolicy(policy.DenyAll()),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("load it"))
	require.NoError(t, err)
	toolEnd, found := findToolEnd(outputs, "skill-1")
	require.True(t, found)
	assert.True(t, toolEnd.IsError)
	assert.Contains(t, textFromParts(toolEnd.Parts), "denied by policy")
}

type skillRequestCapture struct {
	hook.Noop
	mu       sync.Mutex
	requests []*model.Request
}

func (c *skillRequestCapture) BeforeModel(_ context.Context, request *model.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	copyRequest := *request
	copyRequest.Messages = append([]*model.Message(nil), request.Messages...)
	copyRequest.Tools = append([]tools.ToolSpec(nil), request.Tools...)
	c.requests = append(c.requests, &copyRequest)
	return nil
}

func (c *skillRequestCapture) Requests() []*model.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*model.Request(nil), c.requests...)
}

func toolNames(specs []tools.ToolSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}
