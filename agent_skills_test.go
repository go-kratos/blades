package blades

import (
	"testing"

	"github.com/go-kratos/blades/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkRebuildsSkillToolset(t *testing.T) {
	t.Parallel()

	first, err := skills.New(
		skills.Frontmatter{Name: "first-skill", Description: "The first skill."},
		"first instructions",
		skills.Resources{},
	)
	require.NoError(t, err)
	second, err := skills.New(
		skills.Frontmatter{Name: "second-skill", Description: "The second skill."},
		"second instructions",
		skills.Resources{},
	)
	require.NoError(t, err)

	base, err := NewAgent("base", WithModel(testProvider{name: "test"}), WithSkills(first))
	require.NoError(t, err)
	forked, err := Fork(base, WithSkills(second))
	require.NoError(t, err)

	fork := forked.(*llmAgent)
	require.NotNil(t, fork.skillToolset)
	assert.Contains(t, fork.skillToolset.Instruction(), "first-skill")
	assert.Contains(t, fork.skillToolset.Instruction(), "second-skill")
	assert.Len(t, fork.skillToolset.Tools(), 3)
}

func TestNewAgentRejectsInvalidSkill(t *testing.T) {
	t.Parallel()

	invalid := skills.Definition{
		Meta: skills.Frontmatter{Name: "INVALID", Description: "Invalid name."},
	}
	_, err := NewAgent("agent", WithModel(testProvider{name: "test"}), WithSkills(invalid))
	assert.ErrorContains(t, err, "lowercase kebab-case")
}
