package skills

import (
	"strings"
	"testing"
)

func TestFormatSkillsAsXML(t *testing.T) {
	t.Parallel()

	xml := FormatSkillsAsXML([]Skill{
		&staticSkill{frontmatter: Frontmatter{Name: "skill1", Description: "desc1"}},
		&staticSkill{frontmatter: Frontmatter{Name: "skill2", Description: "desc<2>"}},
	})
	if xml == "" {
		t.Fatalf("expected non-empty xml")
	}
	if want := "<available_skills>"; !strings.Contains(xml, want) {
		t.Fatalf("missing %q", want)
	}
	if want := "desc&lt;2&gt;"; !strings.Contains(xml, want) {
		t.Fatalf("missing escaped description")
	}
}

func TestFormatSkillsAsXMLEmpty(t *testing.T) {
	t.Parallel()

	xml := FormatSkillsAsXML(nil)
	if xml != "<available_skills>\n</available_skills>" {
		t.Fatalf("unexpected xml: %q", xml)
	}
}
