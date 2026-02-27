package skills

import (
	"html"
	"strings"
)

// DefaultSystemInstruction is the instruction injected when skills are enabled.
const DefaultSystemInstruction = `You can use specialized 'skills' to help you with complex tasks. You MUST use the skill tools to interact with these skills.

Skills are folders of instructions and resources that extend your capabilities for specialized tasks. Each skill folder contains:
- SKILL.md (required): The main instruction file with skill metadata and detailed markdown instructions.
- references/ (optional): Additional documentation or examples for skill usage.
- assets/ (optional): Templates, docs, or other resources used by the skill.
- scripts/ (optional): Script files that can be inspected with load_skill_resource and executed with run_skill_script.

This is very important:
1. If a skill seems relevant, use load_skill with name="<SKILL_NAME>" to read full instructions first.
2. Once loaded, follow skill instructions exactly before replying.
3. Use load_skill_resource for files inside references/, assets/, and scripts/.
4. Use run_skill_script for scripts under scripts/ when execution is required.`

// FormatSkillsAsXML formats skills metadata as an XML block.
func FormatSkillsAsXML(skills []Skill) string {
	if len(skills) == 0 {
		return "<available_skills>\n</available_skills>"
	}
	lines := []string{"<available_skills>"}
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		lines = append(lines,
			"<skill>",
			"<name>",
			html.EscapeString(skill.Name()),
			"</name>",
			"<description>",
			html.EscapeString(skill.Description()),
			"</description>",
			"</skill>",
		)
	}
	lines = append(lines, "</available_skills>")
	return strings.Join(lines, "\n")
}
