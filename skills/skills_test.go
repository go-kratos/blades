package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSkillsAsXMLEscapesCatalogValues(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "safe-name", Description: `Use when A < B & B > C`},
		"full instructions must not appear in the catalog",
		Resources{},
	)
	require.NoError(t, err)

	catalog := FormatSkillsAsXML([]Skill{skill})
	assert.Contains(t, catalog, "Use when A &lt; B &amp; B &gt; C")
	assert.NotContains(t, catalog, "full instructions")
}

func TestNewToolsetHandlesEmptyAndDuplicateSkills(t *testing.T) {
	t.Parallel()

	empty, err := NewToolset([]Skill{nil})
	require.NoError(t, err)
	assert.Empty(t, empty.Tools())
	assert.Empty(t, empty.Instruction())

	first, err := New(Frontmatter{Name: "same-skill", Description: "First."}, "first", Resources{})
	require.NoError(t, err)
	second, err := New(Frontmatter{Name: "same-skill", Description: "Second."}, "second", Resources{})
	require.NoError(t, err)
	_, err = NewToolset([]Skill{first, second})
	assert.ErrorContains(t, err, `duplicate skill name "same-skill"`)
}

func TestToolsetDisclosesSkillProgressively(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "add-friend", Description: "Use when the user wants to add a friend."},
		"Search first, then ask the user to confirm before adding.",
		Resources{
			References: map[string]string{"flow.md": "confirmation is required"},
			Assets:     map[string][]byte{"icon.bin": {0xff, 0x00}},
		},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{skill})
	require.NoError(t, err)

	assert.Contains(t, toolset.Instruction(), "add-friend")
	assert.NotContains(t, toolset.Instruction(), "Search first")

	runtime := toolset.NewRuntime()
	toolByName := make(map[string]tools.Tool)
	for _, tool := range runtime.Tools() {
		toolByName[tool.Spec().Name] = tool
	}

	loaded, err := toolByName[ToolLoadSkillName].Handle(context.Background(), json.RawMessage(`{"name":"add-friend"}`))
	require.NoError(t, err)
	assert.Contains(t, content.TextFromParts(loaded.Parts), "Search first")
	assert.Contains(t, content.TextFromParts(loaded.Parts), "references")
	for _, tool := range runtime.DisclosureFromMessages(skillLoadResultHistory(loaded)).FilterTools(runtime.Tools()) {
		toolByName[tool.Spec().Name] = tool
	}

	reference, err := toolByName[ToolLoadSkillResourceName].Handle(context.Background(), json.RawMessage(`{"skill_name":"add-friend","path":"references/flow.md"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"skill_name":"add-friend","path":"references/flow.md","encoding":"utf-8","content":"confirmation is required"}`, content.TextFromParts(reference.Parts))

	asset, err := toolByName[ToolLoadSkillResourceName].Handle(context.Background(), json.RawMessage(`{"skill_name":"add-friend","path":"assets/icon.bin"}`))
	require.NoError(t, err)
	var assetResult map[string]any
	require.NoError(t, json.Unmarshal([]byte(content.TextFromParts(asset.Parts)), &assetResult))
	assert.Equal(t, "base64", assetResult["encoding"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte{0xff, 0x00}), assetResult["content_base64"])
}

func TestListSkillsSupportsProviderCompatibleFiltering(t *testing.T) {
	t.Parallel()

	addFriend, err := New(
		Frontmatter{Name: "add-friend", Description: "Use when the user wants to add a friend."},
		"add friend instructions",
		Resources{},
	)
	require.NoError(t, err)
	weather, err := New(
		Frontmatter{Name: "weather", Description: "Look up a forecast."},
		"weather instructions",
		Resources{},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{addFriend, weather})
	require.NoError(t, err)
	listTool := toolset.Tools()[0]

	schema, err := json.Marshal(listTool.Spec().InputSchema)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{
			"query":{
				"type":"string",
				"description":"Optional case-insensitive substring used to filter skill names and descriptions."
			}
		}
	}`, string(schema))

	result, err := listTool.Handle(context.Background(), json.RawMessage(`{"query":"FRIEND"}`))
	require.NoError(t, err)
	text := content.TextFromParts(result.Parts)
	assert.Contains(t, text, "add-friend")
	assert.NotContains(t, text, "weather")
}

func TestRuntimeDisclosesOnlyToolsFromLoadedSkills(t *testing.T) {
	t.Parallel()

	addFriend, err := New(
		Frontmatter{
			Name: "add-friend", Description: "Use when the user wants to add a friend.",
			AllowedTools: "friend_search, friend_add",
		},
		"Search first, then add after confirmation.",
		Resources{},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{addFriend})
	require.NoError(t, err)
	runtime := toolset.NewRuntime()
	allTools := append([]tools.Tool{
		namedSkillTestTool("current_time"),
		namedSkillTestTool("friend_search"),
		namedSkillTestTool("friend_add"),
	}, runtime.Tools()...)

	beforeLoad := runtime.DisclosureFromMessages(nil)
	assert.ElementsMatch(t, []string{
		"current_time", ToolListSkillsName, ToolLoadSkillName, ToolLoadSkillResourceName,
	}, skillTestToolNames(beforeLoad.FilterTools(allTools)))

	loadResult, err := runtime.Tools()[1].Handle(context.Background(), json.RawMessage(`{"name":"add-friend"}`))
	require.NoError(t, err)
	afterLoad := runtime.DisclosureFromMessages(skillLoadResultHistory(loadResult))
	assert.ElementsMatch(t, []string{
		"current_time", "friend_search", "friend_add",
		ToolListSkillsName, ToolLoadSkillName, ToolLoadSkillResourceName,
	}, skillTestToolNames(afterLoad.FilterTools(allTools)))

	assert.NotContains(t, skillTestToolNames(beforeLoad.FilterTools(allTools)), "friend_search")
	assert.NotContains(t, skillTestToolNames(toolset.NewRuntime().DisclosureFromMessages(nil).FilterTools(allTools)), "friend_search")
}

func TestNewToolsetRejectsInvalidAllowedToolPattern(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "invalid-tools", Description: "Invalid allowed tools.", AllowedTools: "[invalid"},
		"instructions",
		Resources{},
	)
	require.NoError(t, err)
	_, err = NewToolset([]Skill{skill})
	assert.ErrorContains(t, err, "invalid allowed-tools pattern")
}

func TestLoadSkillResourceRejectsTraversal(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "safe-skill", Description: "A safe skill."},
		"instructions",
		Resources{References: map[string]string{"inside.md": "inside"}},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{skill})
	require.NoError(t, err)

	resourceTool := toolset.Tools()[2]
	for _, resourcePath := range []string{"../secret", "references/../secret", `references\..\secret`} {
		input, err := json.Marshal(map[string]string{"skill_name": "safe-skill", "path": resourcePath})
		require.NoError(t, err)
		result, err := resourceTool.Handle(context.Background(), input)
		assert.Nil(t, result)
		assert.ErrorContains(t, err, "INVALID_RESOURCE_PATH")
	}
}

func TestLoadSkillResourceRequiresLoadedSkill(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "support", Description: "Support procedures."},
		"Follow the support procedure.",
		Resources{References: map[string]string{"guide.md": "guide"}},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{skill})
	require.NoError(t, err)
	runtime := toolset.NewRuntime()

	_, err = runtime.Tools()[2].Handle(context.Background(), json.RawMessage(`{"skill_name":"support","path":"references/guide.md"}`))
	assert.ErrorContains(t, err, "SKILL_NOT_LOADED")

	loadResult, err := runtime.Tools()[1].Handle(context.Background(), json.RawMessage(`{"name":"support"}`))
	require.NoError(t, err)
	toolsAfterLoad := runtime.DisclosureFromMessages(skillLoadResultHistory(loadResult)).FilterTools(runtime.Tools())
	result, err := toolsAfterLoad[2].Handle(context.Background(), json.RawMessage(`{"skill_name":"support","path":"references/guide.md"}`))
	require.NoError(t, err)
	assert.Contains(t, content.TextFromParts(result.Parts), "guide")
}

func TestRuntimeRestoresMatchingSkillFromHistory(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "add-friend", Description: "Add a friend.", AllowedTools: "friend_search"},
		"Search before adding.",
		Resources{},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{skill})
	require.NoError(t, err)
	loadedRuntime := toolset.NewRuntime()
	result, err := loadedRuntime.Tools()[1].Handle(context.Background(), json.RawMessage(`{"name":"add-friend"}`))
	require.NoError(t, err)
	history := []*model.Message{{
		Role: model.RoleTool,
		Parts: []content.Part{content.ToolResult{
			ID: "skill-1", Name: ToolLoadSkillName, Parts: result.Parts,
		}},
	}}

	restoredRuntime := toolset.NewRuntime()
	assert.True(t, restoredRuntime.DisclosureFromMessages(history).AllowsTool("friend_search"))

	changedSkill, err := New(
		Frontmatter{Name: "add-friend", Description: "Add a friend.", AllowedTools: "friend_search"},
		"Use the new procedure.",
		Resources{},
	)
	require.NoError(t, err)
	changedToolset, err := NewToolset([]Skill{changedSkill})
	require.NoError(t, err)
	changedRuntime := changedToolset.NewRuntime()
	assert.False(t, changedRuntime.DisclosureFromMessages(history).AllowsTool("friend_search"))
}

func skillLoadResultHistory(result *tools.Result) []*model.Message {
	return []*model.Message{{
		Role: model.RoleTool,
		Parts: []content.Part{content.ToolResult{
			ID: "skill-1", Name: ToolLoadSkillName, Parts: result.Parts,
		}},
	}}
}

func TestSkillToolFailuresReturnErrors(t *testing.T) {
	t.Parallel()

	skill, err := New(
		Frontmatter{Name: "safe-skill", Description: "A safe skill."},
		"instructions",
		Resources{References: map[string]string{"inside.md": "inside"}},
	)
	require.NoError(t, err)
	toolset, err := NewToolset([]Skill{skill})
	require.NoError(t, err)
	runtime := toolset.NewRuntime()
	toolsByName := make(map[string]tools.Tool)
	for _, tool := range runtime.Tools() {
		toolsByName[tool.Spec().Name] = tool
	}
	loadResult, err := toolsByName[ToolLoadSkillName].Handle(context.Background(), json.RawMessage(`{"name":"safe-skill"}`))
	require.NoError(t, err)
	for _, tool := range runtime.DisclosureFromMessages(skillLoadResultHistory(loadResult)).FilterTools(runtime.Tools()) {
		toolsByName[tool.Spec().Name] = tool
	}

	tests := []struct {
		name      string
		tool      tools.Tool
		input     json.RawMessage
		errorCode string
	}{
		{name: "invalid load arguments", tool: toolsByName[ToolLoadSkillName], input: json.RawMessage(`{`), errorCode: "INVALID_ARGUMENTS"},
		{name: "invalid list arguments", tool: toolsByName[ToolListSkillsName], input: json.RawMessage(`{`), errorCode: "INVALID_ARGUMENTS"},
		{name: "missing skill name", tool: toolsByName[ToolLoadSkillName], input: json.RawMessage(`{}`), errorCode: "MISSING_SKILL_NAME"},
		{name: "unknown skill", tool: toolsByName[ToolLoadSkillName], input: json.RawMessage(`{"name":"unknown"}`), errorCode: "SKILL_NOT_FOUND"},
		{name: "invalid resource arguments", tool: toolsByName[ToolLoadSkillResourceName], input: json.RawMessage(`{`), errorCode: "INVALID_ARGUMENTS"},
		{name: "missing resource arguments", tool: toolsByName[ToolLoadSkillResourceName], input: json.RawMessage(`{}`), errorCode: "INVALID_ARGUMENTS"},
		{name: "unknown resource skill", tool: toolsByName[ToolLoadSkillResourceName], input: json.RawMessage(`{"skill_name":"unknown","path":"references/a.md"}`), errorCode: "SKILL_NOT_FOUND"},
		{name: "invalid resource path", tool: toolsByName[ToolLoadSkillResourceName], input: json.RawMessage(`{"skill_name":"safe-skill","path":"../secret"}`), errorCode: "INVALID_RESOURCE_PATH"},
		{name: "missing resource", tool: toolsByName[ToolLoadSkillResourceName], input: json.RawMessage(`{"skill_name":"safe-skill","path":"references/missing.md"}`), errorCode: "RESOURCE_NOT_FOUND"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.tool.Handle(context.Background(), test.input)
			assert.Nil(t, result)
			assert.ErrorContains(t, err, test.errorCode)
		})
	}
}

func TestNewFromEmbedLoadsStandardSkillLayout(t *testing.T) {
	t.Parallel()

	fileSystem := fstest.MapFS{
		"add-friend/SKILL.md":          &fstest.MapFile{Data: []byte("---\nname: add-friend\ndescription: Add a friend safely.\nmetadata:\n  owner: social\n---\n# Steps\nSearch first.")},
		"add-friend/references/api.md": &fstest.MapFile{Data: []byte("API reference")},
		"add-friend/assets/icon.png":   &fstest.MapFile{Data: []byte{0x89, 0x50}},
		"add-friend/scripts/check.sh":  &fstest.MapFile{Data: []byte("echo ok")},
	}

	loaded, err := NewFromEmbed(fileSystem)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "add-friend", loaded[0].Name())
	assert.Equal(t, "# Steps\nSearch first.", loaded[0].Instruction())
	resources := loaded[0].(ResourcesProvider).Resources()
	assert.Equal(t, []string{"api.md"}, resources.ListReferences())
	assert.Equal(t, []string{"icon.png"}, resources.ListAssets())
	assert.Equal(t, []string{"check.sh"}, resources.ListScripts())
}

type namedSkillTestTool string

func (t namedSkillTestTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: string(t), Description: string(t)}
}

func (namedSkillTestTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	return tools.TextResult("ok"), nil
}

func skillTestToolNames(toolList []tools.Tool) []string {
	names := make([]string, 0, len(toolList))
	for _, tool := range toolList {
		names = append(names, tool.Spec().Name)
	}
	return names
}
