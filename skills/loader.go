package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// NewFromDir loads all skills found below a local directory.
func NewFromDir(dir string) ([]Skill, error) {
	return loadAll(os.DirFS(dir))
}

// NewFromEmbed loads all skills found in an fs.FS.
func NewFromEmbed(fsys fs.FS) ([]Skill, error) {
	return loadAll(fsys)
}

// ReadSkillFrontmatter reads and validates one local skill without loading its body.
func ReadSkillFrontmatter(dir string) (Frontmatter, error) {
	frontmatter, _, err := parseSkillMarkdown(os.DirFS(dir), ".")
	if err != nil {
		return Frontmatter{}, err
	}
	if err := validateRootName(".", filepath.Base(filepath.Clean(dir)), frontmatter.Name); err != nil {
		return Frontmatter{}, err
	}
	return frontmatter, nil
}

func loadAll(fsys fs.FS) ([]Skill, error) {
	roots, err := findSkillRoots(fsys)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("skills: SKILL.md not found")
	}
	result := make([]Skill, 0, len(roots))
	rootByName := make(map[string]string, len(roots))
	for _, root := range roots {
		skill, err := loadSkill(fsys, root)
		if err != nil {
			return nil, fmt.Errorf("skills: load %q: %w", root, err)
		}
		if err := validateRootName(root, "", skill.Name()); err != nil {
			return nil, fmt.Errorf("skills: load %q: %w", root, err)
		}
		if previous, exists := rootByName[skill.Name()]; exists {
			return nil, fmt.Errorf("skills: duplicate skill name %q in %q and %q", skill.Name(), previous, root)
		}
		rootByName[skill.Name()] = root
		result = append(result, skill)
	}
	return result, nil
}

func findSkillRoots(fsys fs.FS) ([]string, error) {
	candidates := make(map[string]struct{})
	if err := fs.WalkDir(fsys, ".", func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "SKILL.md" || entry.Name() == "skill.md" {
			candidates[path.Dir(filePath)] = struct{}{}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(candidates))
	for root := range candidates {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		if nestedUnderResource(root, result) {
			continue
		}
		result = append(result, root)
	}
	return result, nil
}

func nestedUnderResource(candidate string, roots []string) bool {
	for _, root := range roots {
		for _, directory := range []string{"references", "assets", "scripts"} {
			resourceRoot := path.Clean(path.Join(root, directory))
			if candidate == resourceRoot || strings.HasPrefix(candidate, resourceRoot+"/") {
				return true
			}
		}
	}
	return false
}

func validateRootName(root, dotRootName, skillName string) error {
	expected := path.Base(root)
	if root == "." {
		expected = dotRootName
	}
	if expected == "" || expected == "." {
		return nil
	}
	if expected != skillName {
		return fmt.Errorf("skills: skill name %q does not match directory name %q", skillName, expected)
	}
	return nil
}

func loadSkill(fsys fs.FS, root string) (Skill, error) {
	frontmatter, instruction, err := parseSkillMarkdown(fsys, root)
	if err != nil {
		return nil, err
	}
	references, err := readTextFiles(fsys, path.Join(root, "references"))
	if err != nil {
		return nil, err
	}
	assets, err := readBinaryFiles(fsys, path.Join(root, "assets"))
	if err != nil {
		return nil, err
	}
	scripts, err := readTextFiles(fsys, path.Join(root, "scripts"))
	if err != nil {
		return nil, err
	}
	return New(frontmatter, instruction, Resources{
		References: references,
		Assets:     assets,
		Scripts:    scripts,
	})
}

func parseSkillMarkdown(fsys fs.FS, root string) (Frontmatter, string, error) {
	data, err := readSkillMarkdown(fsys, root)
	if err != nil {
		return Frontmatter{}, "", err
	}
	frontmatterBlock, body, err := splitFrontmatter(data)
	if err != nil {
		return Frontmatter{}, "", err
	}
	frontmatter, err := parseFrontmatter(frontmatterBlock)
	if err != nil {
		return Frontmatter{}, "", err
	}
	if err := frontmatter.Validate(); err != nil {
		return Frontmatter{}, "", err
	}
	return frontmatter, strings.TrimSpace(body), nil
}

func readSkillMarkdown(fsys fs.FS, root string) (string, error) {
	for _, filename := range []string{"SKILL.md", "skill.md"} {
		data, err := fs.ReadFile(fsys, path.Join(root, filename))
		if err == nil {
			return string(data), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("skills: SKILL.md not found")
}

func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return "", "", fmt.Errorf("skills: SKILL.md must start with YAML frontmatter")
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSuffix(lines[index], "\r") != "---" {
			continue
		}
		return strings.Join(lines[1:index], "\n"), strings.Join(lines[index+1:], "\n"), nil
	}
	return "", "", fmt.Errorf("skills: SKILL.md frontmatter not properly closed with ---")
}

func parseFrontmatter(content string) (Frontmatter, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return Frontmatter{}, fmt.Errorf("skills: invalid YAML in frontmatter: %w", err)
	}
	name, ok := raw["name"].(string)
	if !ok {
		return Frontmatter{}, fmt.Errorf("skills: name must be a string")
	}
	description, ok := raw["description"].(string)
	if !ok {
		return Frontmatter{}, fmt.Errorf("skills: description must be a string")
	}
	frontmatter := Frontmatter{Name: name, Description: description}
	var err error
	if frontmatter.License, err = optionalString(raw, "license"); err != nil {
		return Frontmatter{}, err
	}
	if frontmatter.Compatibility, err = optionalString(raw, "compatibility"); err != nil {
		return Frontmatter{}, err
	}
	if raw["allowed-tools"] != nil {
		frontmatter.AllowedTools, err = optionalString(raw, "allowed-tools")
	} else {
		frontmatter.AllowedTools, err = optionalString(raw, "allowed_tools")
	}
	if err != nil {
		return Frontmatter{}, err
	}
	if raw["metadata"] != nil {
		frontmatter.Metadata, err = normalizeMetadataMap(raw["metadata"])
		if err != nil {
			return Frontmatter{}, err
		}
	}
	return frontmatter, nil
}

func optionalString(values map[string]any, key string) (string, error) {
	value, exists := values[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("skills: %s must be a string", key)
	}
	return text, nil
}

func readTextFiles(fsys fs.FS, directory string) (map[string]string, error) {
	files, err := readFiles(fsys, directory)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(files))
	for name, content := range files {
		result[name] = string(content)
	}
	return result, nil
}

func readBinaryFiles(fsys fs.FS, directory string) (map[string][]byte, error) {
	return readFiles(fsys, directory)
}

func readFiles(fsys fs.FS, directory string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := fs.WalkDir(fsys, directory, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}
		result[strings.TrimPrefix(filePath, directory+"/")] = content
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return result, nil
}
