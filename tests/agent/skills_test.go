package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/hook"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/session"
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

func TestAgentMarksSkillToolFailureAsError(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "known-skill", Description: "A known skill."},
		"known instructions",
		skills.Resources{},
	)
	require.NoError(t, err)
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"unknown-skill"}`)),
		dummyprovider.TextResponse("That skill is unavailable."),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithSkills(skill),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("load it"))
	require.NoError(t, err)
	toolEnd, found := findToolEnd(outputs, "skill-1")
	require.True(t, found)
	assert.True(t, toolEnd.IsError)
	assert.Contains(t, textFromParts(toolEnd.Parts), "SKILL_NOT_FOUND")
}

func TestAgentDisclosesSkillToolsAfterLoading(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{
			Name: "add-friend", Description: "Use when the user asks to add a friend.",
			AllowedTools: "friend_search",
		},
		"Search for the friend before replying.",
		skills.Resources{},
	)
	require.NoError(t, err)
	friendSearch := &countedSkillTool{name: "friend_search"}
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"add-friend"}`)),
		dummyprovider.ToolUseResponse("tool-1", "friend_search", json.RawMessage(`{"query":"Alice"}`)),
		dummyprovider.TextResponse("Alice found."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(friendSearch),
		blades.WithSkills(skill),
		blades.WithHooks(capture),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("Add Alice as a friend"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), friendSearch.calls.Load())
	assert.Equal(t, "Alice found.", turnEnds(outputs)[2].Text())

	requests := capture.Requests()
	require.Len(t, requests, 3)
	assert.NotContains(t, toolNames(requests[0].Tools), "friend_search")
	assert.Contains(t, toolNames(requests[1].Tools), "friend_search")
}

func TestAgentRejectsSkillToolLoadedInSameWave(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{
			Name: "add-friend", Description: "Use when the user asks to add a friend.",
			AllowedTools: "friend_add",
		},
		"Add the selected friend.",
		skills.Resources{},
	)
	require.NoError(t, err)
	friendAdd := &countedSkillTool{name: "friend_add"}
	provider := dummyprovider.New(
		dummyprovider.AssistantResponse([]content.Part{
			dummyprovider.ToolUse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"add-friend"}`)),
			dummyprovider.ToolUse("tool-1", "friend_add", json.RawMessage(`{"user_id":"1001"}`)),
		}, dummyprovider.WithStopReason(model.StopToolUse)),
		dummyprovider.TextResponse("I need to call the tool in the next wave."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(friendAdd),
		blades.WithSkills(skill),
		blades.WithHooks(capture),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("Add Alice"))
	require.NoError(t, err)
	assert.Zero(t, friendAdd.calls.Load())
	toolEnd, found := findToolEnd(outputs, "tool-1")
	require.True(t, found)
	assert.True(t, toolEnd.IsError)
	assert.Contains(t, textFromParts(toolEnd.Parts), "tool not found")

	requests := capture.Requests()
	require.Len(t, requests, 2)
	assert.NotContains(t, toolNames(requests[0].Tools), "friend_add")
	assert.Contains(t, toolNames(requests[1].Tools), "friend_add")
}

func TestAgentRestoresSkillToolsFromCarriedHistory(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{
			Name: "add-friend", Description: "Use when the user asks to add a friend.",
			AllowedTools: "friend_search",
		},
		"Search for the friend before replying.",
		skills.Resources{},
	)
	require.NoError(t, err)
	loadResult := `{"frontmatter":{"name":"add-friend","description":"Use when the user asks to add a friend.","allowed-tools":"friend_search"},"instructions":"Search for the friend before replying.","resources":{"assets":[],"references":[],"scripts":[]},"skill_name":"add-friend"}`
	sess := session.NewSession(session.WithMessages(
		&model.Message{Role: model.RoleAssistant, Parts: []content.Part{
			content.ToolUse{ID: "skill-previous", Name: skills.ToolLoadSkillName, Input: json.RawMessage(`{"name":"add-friend"}`)},
		}},
		&model.Message{Role: model.RoleTool, Parts: []content.Part{
			content.ToolResult{ID: "skill-previous", Name: skills.ToolLoadSkillName, Parts: []content.Part{content.Text{Text: loadResult}}},
		}},
	))
	friendSearch := &countedSkillTool{name: "friend_search"}
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("tool-1", "friend_search", json.RawMessage(`{"query":"Alice"}`)),
		dummyprovider.TextResponse("Alice found."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(friendSearch),
		blades.WithSkills(skill),
		blades.WithHooks(capture),
	)
	require.NoError(t, err)

	ctx := session.NewContext(context.Background(), sess)
	outputs, err := collectAllAgentOutputs(ctx, agent, promptInputs("Use the previous result"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), friendSearch.calls.Load())
	assert.Equal(t, "Alice found.", turnEnds(outputs)[1].Text())

	requests := capture.Requests()
	require.Len(t, requests, 2)
	assert.Contains(t, toolNames(requests[0].Tools), "friend_search")
}

func TestAgentDoesNotDiscloseToolsAfterLoadResultIsRewritten(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "add-friend", Description: "Add a friend.", AllowedTools: "friend_search"},
		"Search before adding.",
		skills.Resources{},
	)
	require.NoError(t, err)
	friendSearch := &countedSkillTool{name: "friend_search"}
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"add-friend"}`)),
		dummyprovider.ToolUseResponse("tool-1", "friend_search", json.RawMessage(`{"query":"Alice"}`)),
		dummyprovider.TextResponse("The Skill result was unavailable."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(friendSearch),
		blades.WithSkills(skill),
		blades.WithHooks(&rewriteToolResultHook{}, capture),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("Add Alice"))
	require.NoError(t, err)
	assert.Zero(t, friendSearch.calls.Load())
	toolEnd, found := findToolEnd(outputs, "tool-1")
	require.True(t, found)
	assert.True(t, toolEnd.IsError)

	requests := capture.Requests()
	require.Len(t, requests, 3)
	assert.NotContains(t, toolNames(requests[1].Tools), "friend_search")
}

func TestAgentRecomputesDisclosureAfterCompaction(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "add-friend", Description: "Add a friend.", AllowedTools: "friend_search"},
		"Search before adding.",
		skills.Resources{},
	)
	require.NoError(t, err)
	loadResult := `{"frontmatter":{"name":"add-friend","description":"Add a friend.","allowed-tools":"friend_search"},"instructions":"Search before adding.","resources":{"assets":[],"references":[],"scripts":[]},"skill_name":"add-friend"}`
	sess := session.NewSession(session.WithMessages(
		&model.Message{Role: model.RoleTool, Parts: []content.Part{
			content.ToolResult{ID: "skill-old", Name: skills.ToolLoadSkillName, Parts: []content.Part{content.Text{Text: loadResult}}},
		}},
	))
	friendSearch := &countedSkillTool{name: "friend_search"}
	provider := dummyprovider.New(
		dummyprovider.ToolUseResponse("tool-1", "friend_search", json.RawMessage(`{"query":"Alice"}`)),
		dummyprovider.TextResponse("The old Skill was compacted."),
	)
	capture := &skillRequestCapture{}
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithTools(friendSearch),
		blades.WithSkills(skill),
		blades.WithHooks(capture),
		blades.WithCompact(compact.CompactorFunc(func(_ context.Context, request compact.Request) ([]*model.Message, error) {
			return request.Messages[1:], nil
		})),
		blades.WithContextWindow(model.ContextWindow{MaxTokens: 14_000}),
		blades.WithTokenCounter(model.TokenCounterFunc(func(context.Context, *model.Request) (model.TokenCount, error) {
			return model.TokenCount{Input: 10_000}, nil
		})),
	)
	require.NoError(t, err)

	ctx := session.NewContext(context.Background(), sess)
	outputs, err := collectAllAgentOutputs(ctx, agent, promptInputs("Continue"))
	require.NoError(t, err)
	assert.Zero(t, friendSearch.calls.Load())
	toolEnd, found := findToolEnd(outputs, "tool-1")
	require.True(t, found)
	assert.True(t, toolEnd.IsError)

	requests := capture.Requests()
	require.Len(t, requests, 2)
	assert.NotContains(t, toolNames(requests[0].Tools), "friend_search")
}

func TestAgentFreezesSkillResourceEligibilityForToolWave(t *testing.T) {
	t.Parallel()

	skill, err := skills.New(
		skills.Frontmatter{Name: "support", Description: "Support procedures."},
		"Read the guide.",
		skills.Resources{References: map[string]string{"guide.md": "guide content"}},
	)
	require.NoError(t, err)
	provider := dummyprovider.New(
		dummyprovider.AssistantResponse([]content.Part{
			dummyprovider.ToolUse("skill-1", skills.ToolLoadSkillName, json.RawMessage(`{"name":"support"}`)),
			dummyprovider.ToolUse("resource-early", skills.ToolLoadSkillResourceName, json.RawMessage(`{"skill_name":"support","path":"references/guide.md"}`)),
		}, dummyprovider.WithStopReason(model.StopToolUse)),
		dummyprovider.ToolUseResponse("resource-next", skills.ToolLoadSkillResourceName, json.RawMessage(`{"skill_name":"support","path":"references/guide.md"}`)),
		dummyprovider.TextResponse("Guide loaded."),
	)
	agent, err := blades.NewAgent(
		"assistant",
		blades.WithModel(provider),
		blades.WithSkills(skill),
	)
	require.NoError(t, err)

	outputs, err := collectAllAgentOutputs(context.Background(), agent, promptInputs("Load the guide"))
	require.NoError(t, err)
	early, found := findToolEnd(outputs, "resource-early")
	require.True(t, found)
	assert.True(t, early.IsError)
	assert.Contains(t, textFromParts(early.Parts), "SKILL_NOT_LOADED")
	next, found := findToolEnd(outputs, "resource-next")
	require.True(t, found)
	assert.False(t, next.IsError)
	assert.Contains(t, textFromParts(next.Parts), "guide content")
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

type countedSkillTool struct {
	name  string
	calls atomic.Int32
}

func (t *countedSkillTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: t.name, Description: t.name}
}

func (t *countedSkillTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	t.calls.Add(1)
	return tools.TextResult("ok"), nil
}
