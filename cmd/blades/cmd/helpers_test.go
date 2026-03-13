package cmd

import (
	"testing"

	bladeskills "github.com/go-kratos/blades/skills"

	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func TestInitCreatesLoadableBuiltInSkills(t *testing.T) {
	t.Parallel()

	ws := workspace.New(t.TempDir())
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	skillList, err := bladeskills.NewFromDir(ws.SkillsDir())
	if err != nil {
		t.Fatalf("load built-in skills: %v", err)
	}

	for _, skill := range skillList {
		if skill.Name() == "blades-cron" {
			return
		}
	}

	t.Fatalf("expected built-in skill %q in %s", "blades-cron", ws.SkillsDir())
}
