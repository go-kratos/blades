package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/go-kratos/blades/content"
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

	toolByName := make(map[string]tools.Tool)
	for _, tool := range toolset.Tools() {
		toolByName[tool.Spec().Name] = tool
	}

	loaded, err := toolByName[ToolLoadSkillName].Handle(context.Background(), json.RawMessage(`{"name":"add-friend"}`))
	require.NoError(t, err)
	assert.Contains(t, content.TextFromParts(loaded.Parts), "Search first")
	assert.Contains(t, content.TextFromParts(loaded.Parts), "references")

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
		require.NoError(t, err)
		assert.Contains(t, content.TextFromParts(result.Parts), "INVALID_RESOURCE_PATH")
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
