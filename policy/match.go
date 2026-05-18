package policy

import "path"

// ToolName matches by tool name glob pattern.
func ToolName(pattern string) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		matched, _ := path.Match(pattern, req.Tool.Spec().Name)
		return matched
	})
}

// ToolNames matches if the tool name matches any of the given patterns.
func ToolNames(patterns ...string) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		name := req.Tool.Spec().Name
		for _, p := range patterns {
			if matched, _ := path.Match(p, name); matched {
				return true
			}
		}
		return false
	})
}

// All requires all matchers to match (AND).
func All(matchers ...Matcher) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		for _, m := range matchers {
			if !m.Match(req) {
				return false
			}
		}
		return true
	})
}

// Any requires at least one matcher to match (OR).
func Any(matchers ...Matcher) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		for _, m := range matchers {
			if m.Match(req) {
				return true
			}
		}
		return false
	})
}

// Not inverts a matcher.
func Not(m Matcher) Matcher {
	return MatcherFunc(func(req ToolRequest) bool {
		return !m.Match(req)
	})
}
