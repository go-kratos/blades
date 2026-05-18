package policy

import (
	"encoding/json"
	"path"
	"strings"
)

// Predicate tests a string value extracted from tool input.
type Predicate func(string) bool

// Input matches when a top-level string field in the tool input satisfies pred.
func Input(field string, pred Predicate) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		if len(req.Input) == 0 {
			return false
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(req.Input, &m); err != nil {
			return false
		}
		raw, ok := m[field]
		if !ok {
			return false
		}
		var val string
		if err := json.Unmarshal(raw, &val); err != nil {
			return false
		}
		return pred(val)
	})
}

// HasPrefix returns a predicate that checks for a string prefix.
func HasPrefix(prefix string) Predicate {
	return func(s string) bool { return strings.HasPrefix(s, prefix) }
}

// HasSuffix returns a predicate that checks for a string suffix.
func HasSuffix(suffix string) Predicate {
	return func(s string) bool { return strings.HasSuffix(s, suffix) }
}

// Contains returns a predicate that checks for substring presence.
func Contains(substr string) Predicate {
	return func(s string) bool { return strings.Contains(s, substr) }
}

// Equals returns a predicate for exact string match.
func Equals(target string) Predicate {
	return func(s string) bool { return s == target }
}

// Glob returns a predicate that matches a glob pattern using path.Match.
func Glob(pattern string) Predicate {
	return func(s string) bool {
		matched, _ := path.Match(pattern, s)
		return matched
	}
}

// AnyOf returns a predicate that matches if any sub-predicate matches.
func AnyOf(preds ...Predicate) Predicate {
	return func(s string) bool {
		for _, p := range preds {
			if p(s) {
				return true
			}
		}
		return false
	}
}
