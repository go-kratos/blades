package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

//go:embed testdata/embedded-skill/*
var embeddedSkillFS embed.FS

func TestNewFromDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "test-skill")
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: test-skill
description: Test description
allowed-tools: "search-*"
---
Do this.`), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "ref.md"), []byte("reference"), 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "assets", "asset.txt"), []byte("asset"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("echo hi"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	skillList, err := NewFromDir(skillDir)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if len(skillList) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skillList))
	}
	skill := skillList[0]
	if skill.Name() != "test-skill" {
		t.Fatalf("unexpected skill name: %s", skill.Name())
	}
	if skill.Frontmatter.AllowedTools != "search-*" {
		t.Fatalf("unexpected allowed-tools: %s", skill.Frontmatter.AllowedTools)
	}
	if _, ok := skill.Resources.GetReference("ref.md"); !ok {
		t.Fatalf("expected ref.md")
	}
	if _, ok := skill.Resources.GetAsset("asset.txt"); !ok {
		t.Fatalf("expected asset.txt")
	}
	if _, ok := skill.Resources.GetScript("run.sh"); !ok {
		t.Fatalf("expected run.sh")
	}
}

func TestNewFromDirNameMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "wrong-dir")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: my-skill
description: desc
---
Body`), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	skills, err := NewFromDir(skillDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "my-skill" {
		t.Fatalf("unexpected skill name: %s", skills[0].Name())
	}
}

func TestNewFromEmbed(t *testing.T) {
	t.Parallel()

	sub, err := fs.Sub(embeddedSkillFS, "testdata/embedded-skill")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	skills, err := NewFromEmbed(sub)
	if err != nil {
		t.Fatalf("load embedded skill: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "embedded-skill" {
		t.Fatalf("unexpected name: %s", skills[0].Name())
	}
	if skills[0].Instructions == "" {
		t.Fatalf("expected instructions")
	}
}

func TestNewFromEmbedDetectSkillRoot(t *testing.T) {
	t.Parallel()

	skills, err := NewFromEmbed(embeddedSkillFS)
	if err != nil {
		t.Fatalf("load embedded skill: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "embedded-skill" {
		t.Fatalf("unexpected skill name: %s", skills[0].Name())
	}
}

func TestNewFromDirMultipleSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "a-skill")
	second := filepath.Join(root, "group", "b-skill")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatalf("mkdir first: %v", err)
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatalf("mkdir second: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "SKILL.md"), []byte(`---
name: a-skill
description: desc a
---
Body A`), 0o644); err != nil {
		t.Fatalf("write first SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, "skill.md"), []byte(`---
name: b-skill
description: desc b
---
Body B`), 0o644); err != nil {
		t.Fatalf("write second skill.md: %v", err)
	}

	skills, err := NewFromDir(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name() != "a-skill" {
		t.Fatalf("unexpected first skill: %s", skills[0].Name())
	}
	if skills[1].Name() != "b-skill" {
		t.Fatalf("unexpected second skill: %s", skills[1].Name())
	}
}

func TestNewFromEmbedMultipleSkills(t *testing.T) {
	t.Parallel()

	skillFS := fstest.MapFS{
		"bundle/a/SKILL.md": &fstest.MapFile{Data: []byte(`---
name: a-skill
description: desc a
---
Body A`)},
		"bundle/b/skill.md": &fstest.MapFile{Data: []byte(`---
name: b-skill
description: desc b
---
Body B`)},
	}
	sub, err := fs.Sub(skillFS, "bundle")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	skills, err := NewFromEmbed(sub)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Name() != "a-skill" {
		t.Fatalf("unexpected first skill: %s", skills[0].Name())
	}
	if skills[1].Name() != "b-skill" {
		t.Fatalf("unexpected second skill: %s", skills[1].Name())
	}
}

func TestNewFromDirNoSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := NewFromDir(root); err == nil || !strings.Contains(err.Error(), "SKILL.md not found") {
		t.Fatalf("expected SKILL.md not found error, got: %v", err)
	}
}

func TestNewFromEmbedNoSkill(t *testing.T) {
	t.Parallel()

	emptyFS := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("no skill")},
	}
	if _, err := NewFromEmbed(emptyFS); err == nil || !strings.Contains(err.Error(), "SKILL.md not found") {
		t.Fatalf("expected SKILL.md not found error, got: %v", err)
	}
}

func TestNewFromDirDuplicateName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatalf("mkdir first: %v", err)
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatalf("mkdir second: %v", err)
	}
	content := []byte(`---
name: dup-skill
description: desc
---
Body`)
	if err := os.WriteFile(filepath.Join(first, "SKILL.md"), content, 0o644); err != nil {
		t.Fatalf("write first SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, "SKILL.md"), content, 0o644); err != nil {
		t.Fatalf("write second SKILL.md: %v", err)
	}

	if _, err := NewFromDir(root); err == nil || !strings.Contains(err.Error(), "duplicate skill name") {
		t.Fatalf("expected duplicate skill name error, got: %v", err)
	}
}
