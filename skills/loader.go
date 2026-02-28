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

// NewFromDir loads all skills discovered under a local directory.
func NewFromDir(dir string) ([]Skill, error) {
	return loadAllFS(os.DirFS(dir))
}

// NewFromEmbed loads all skills discovered from an fs.FS root.
func NewFromEmbed(fsys fs.FS) ([]Skill, error) {
	return loadAllFS(fsys)
}

// ReadSkillFrontmatter reads and validates frontmatter from one local skill directory.
func ReadSkillFrontmatter(dir string) (Frontmatter, error) {
	fsys := os.DirFS(dir)
	frontmatter, _, err := parseSkillMarkdown(fsys, ".")
	if err != nil {
		return Frontmatter{}, err
	}
	if err := validateSkillRootName(".", dirBaseName(dir), frontmatter.Name); err != nil {
		return Frontmatter{}, err
	}
	return frontmatter, nil
}

func loadAllFS(fsys fs.FS) ([]Skill, error) {
	roots, err := detectSkillRoots(fsys)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("skills: SKILL.md not found")
	}
	sort.Strings(roots)
	out := make([]Skill, 0, len(roots))
	nameToRoot := make(map[string]string, len(roots))
	for _, root := range roots {
		skill, err := loadFS(fsys, root)
		if err != nil {
			return nil, fmt.Errorf("skills: load %q: %w", root, err)
		}
		if err := validateSkillRootName(root, "", skill.Name()); err != nil {
			return nil, fmt.Errorf("skills: load %q: %w", root, err)
		}
		if prevRoot, exists := nameToRoot[skill.Name()]; exists {
			return nil, fmt.Errorf("skills: duplicate skill name %q in %q and %q", skill.Name(), prevRoot, root)
		}
		nameToRoot[skill.Name()] = root
		out = append(out, skill)
	}
	return out, nil
}

func detectSkillRoots(fsys fs.FS) ([]string, error) {
	candidates := make(map[string]struct{})
	err := fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch d.Name() {
		case "SKILL.md", "skill.md":
			candidates[path.Dir(filePath)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(candidates))
	for root := range candidates {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if isNestedInSkillResourceDir(root, out) {
			continue
		}
		out = append(out, root)
	}
	return out, nil
}

func isNestedInSkillResourceDir(candidate string, skillRoots []string) bool {
	for _, root := range skillRoots {
		for _, subdir := range []string{"references", "assets", "scripts"} {
			resourceRoot := path.Clean(path.Join(root, subdir))
			if candidate == resourceRoot || strings.HasPrefix(candidate, resourceRoot+"/") {
				return true
			}
		}
	}
	return false
}

func validateSkillRootName(root string, dotRootName string, skillName string) error {
	expectedName, ok := expectedSkillDirName(root, dotRootName)
	if !ok {
		return nil
	}
	if expectedName != skillName {
		return fmt.Errorf("skills: skill name %q does not match directory name %q", skillName, expectedName)
	}
	return nil
}

func expectedSkillDirName(root string, dotRootName string) (string, bool) {
	if root == "." {
		if dotRootName == "" || dotRootName == "." {
			return "", false
		}
		return dotRootName, true
	}
	return path.Base(root), true
}

func dirBaseName(dir string) string {
	clean := filepath.Clean(dir)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func loadFS(fsys fs.FS, root string) (Skill, error) {
	frontmatter, body, err := parseSkillMarkdown(fsys, root)
	if err != nil {
		return nil, err
	}
	references, err := loadDirFiles(fsys, path.Join(root, "references"))
	if err != nil {
		return nil, err
	}
	assets, err := loadDirBinaryFiles(fsys, path.Join(root, "assets"))
	if err != nil {
		return nil, err
	}
	scripts, err := loadDirFiles(fsys, path.Join(root, "scripts"))
	if err != nil {
		return nil, err
	}
	return &staticSkill{
		frontmatter: frontmatter,
		instruction: body,
		resources: Resources{
			References: references,
			Assets:     assets,
			Scripts:    scripts,
		},
	}, nil
}

func parseSkillMarkdown(fsys fs.FS, root string) (Frontmatter, string, error) {
	skillMD, err := readSkillMarkdown(fsys, root)
	if err != nil {
		return Frontmatter{}, "", err
	}
	frontmatterContent, body, err := splitFrontmatterBlock(skillMD)
	if err != nil {
		return Frontmatter{}, "", err
	}
	frontmatter, err := parseFrontmatter(frontmatterContent)
	if err != nil {
		return Frontmatter{}, "", err
	}
	if err := frontmatter.Validate(); err != nil {
		return Frontmatter{}, "", err
	}
	return frontmatter, strings.TrimSpace(body), nil
}

func splitFrontmatterBlock(skillMD string) (string, string, error) {
	firstLine, rest, hasMore := cutLine(skillMD)
	if !isFrontmatterDelimiterLine(firstLine) {
		return "", "", fmt.Errorf("skills: SKILL.md must start with YAML frontmatter")
	}
	if !hasMore {
		return "", "", fmt.Errorf("skills: SKILL.md frontmatter not properly closed with ---")
	}

	search := rest
	frontmatterLen := 0
	for {
		line, remaining, hasNext := cutLine(search)
		if isFrontmatterDelimiterLine(line) {
			return rest[:frontmatterLen], remaining, nil
		}
		if !hasNext {
			break
		}
		frontmatterLen += len(line) + 1
		search = remaining
	}
	return "", "", fmt.Errorf("skills: SKILL.md frontmatter not properly closed with ---")
}

func cutLine(content string) (line string, rest string, hasMore bool) {
	idx := strings.IndexByte(content, '\n')
	if idx < 0 {
		return content, "", false
	}
	return content[:idx], content[idx+1:], true
}

func isFrontmatterDelimiterLine(line string) bool {
	return trimTrailingCarriageReturn(line) == "---"
}

func trimTrailingCarriageReturn(line string) string {
	if strings.HasSuffix(line, "\r") {
		return line[:len(line)-1]
	}
	return line
}

func readSkillMarkdown(fsys fs.FS, root string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		b, err := fs.ReadFile(fsys, path.Join(root, name))
		if err == nil {
			return string(b), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("skills: SKILL.md not found")
}

func parseFrontmatter(content string) (Frontmatter, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return Frontmatter{}, fmt.Errorf("skills: invalid YAML in frontmatter: %w", err)
	}
	if raw == nil {
		return Frontmatter{}, fmt.Errorf("skills: frontmatter must be a mapping")
	}
	f := Frontmatter{
		Metadata: make(map[string]string),
	}
	name, ok := raw["name"].(string)
	if !ok {
		return Frontmatter{}, fmt.Errorf("skills: name must be a string")
	}
	f.Name = name
	description, ok := raw["description"].(string)
	if !ok {
		return Frontmatter{}, fmt.Errorf("skills: description must be a string")
	}
	f.Description = description
	if v, ok := raw["license"]; ok {
		s, ok := v.(string)
		if !ok {
			return Frontmatter{}, fmt.Errorf("skills: license must be a string")
		}
		f.License = s
	}
	if v, ok := raw["compatibility"]; ok {
		s, ok := v.(string)
		if !ok {
			return Frontmatter{}, fmt.Errorf("skills: compatibility must be a string")
		}
		f.Compatibility = s
	}
	switch {
	case raw["allowed-tools"] != nil:
		s, ok := raw["allowed-tools"].(string)
		if !ok {
			return Frontmatter{}, fmt.Errorf("skills: allowed-tools must be a string")
		}
		f.AllowedTools = s
	case raw["allowed_tools"] != nil:
		s, ok := raw["allowed_tools"].(string)
		if !ok {
			return Frontmatter{}, fmt.Errorf("skills: allowed_tools must be a string")
		}
		f.AllowedTools = s
	}
	if v, ok := raw["metadata"]; ok {
		items, ok := v.(map[string]any)
		if !ok {
			return Frontmatter{}, fmt.Errorf("skills: metadata must be a map")
		}
		for key, value := range items {
			s, ok := value.(string)
			if !ok {
				return Frontmatter{}, fmt.Errorf("skills: metadata value for %q must be a string", key)
			}
			f.Metadata[key] = s
		}
	}
	return f, nil
}

func loadDirFiles(fsys fs.FS, dir string) (map[string]string, error) {
	files := make(map[string]string)
	err := fs.WalkDir(fsys, dir, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(filePath, dir+"/")
		files[rel] = string(b)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return files, nil
}

func loadDirBinaryFiles(fsys fs.FS, dir string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := fs.WalkDir(fsys, dir, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(filePath, dir+"/")
		files[rel] = b
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return files, nil
}
