package recipe

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

var missingKeyPattern = regexp.MustCompile(`map has no entry for key "([^"]+)"`)

// renderTemplate renders a Go text/template string with the given data.
// If the template string is empty, it returns an empty string.
func renderTemplate(tmplStr string, data map[string]any) (string, error) {
	return executeTemplate(tmplStr, data, true)
}

// renderTemplatePreservingUnknown renders a template while preserving unknown
// map keys as raw {{.key}} placeholders for later runtime resolution.
func renderTemplatePreservingUnknown(tmplStr string, data map[string]any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	working := cloneMap(data)
	for i := 0; i < 64; i++ {
		out, err := executeTemplate(tmplStr, working, true)
		if err == nil {
			return out, nil
		}
		key, ok := extractMissingKey(err)
		if !ok {
			return "", err
		}
		if _, exists := working[key]; exists {
			return "", err
		}
		working[key] = "{{." + key + "}}"
	}
	return "", fmt.Errorf("recipe: too many unresolved template keys")
}

func executeTemplate(tmplStr string, data map[string]any, strictMissingKey bool) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	t := template.New("recipe")
	if strictMissingKey {
		t = t.Option("missingkey=error")
	}
	t, err := t.Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// resolveParams merges user-provided params with parameter defaults.
// Returns the merged map ready for template rendering.
func resolveParams(params []ParameterSpec, userParams map[string]any) map[string]any {
	merged := make(map[string]any, len(params))
	for _, p := range params {
		if p.Default != nil {
			merged[p.Name] = p.Default
		}
	}
	for k, v := range userParams {
		merged[k] = v
	}
	return merged
}

// hasTemplateActions checks whether the string contains Go template actions
// that reference session state (i.e., {{.something}}).
// This is used to decide whether to pre-render or defer to runtime.
func hasTemplateActions(s string) bool {
	return strings.Contains(s, "{{")
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func extractMissingKey(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	match := missingKeyPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}
