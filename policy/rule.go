package policy

// Rule is a declarative policy rule.
type Rule struct {
	Matcher Matcher
	Action  Action
	Reason  string
}

// Matcher determines whether a rule applies to a given request.
type Matcher interface {
	Match(req ToolRequest) bool
}

// MatcherFunc is a function adapter for Matcher.
type MatcherFunc func(ToolRequest) bool

func (f MatcherFunc) Match(req ToolRequest) bool { return f(req) }
