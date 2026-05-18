package policy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/policy"
	"github.com/go-kratos/blades/tools"
)

type stubTool struct {
	name string
}

func (s stubTool) Spec() tools.ToolSpec { return tools.ToolSpec{Name: s.name} }
func (s stubTool) Handle(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	return nil, nil
}

func req(name string) policy.ToolRequest {
	return policy.ToolRequest{Tool: stubTool{name: name}}
}

func TestToolNameMatcher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"Bash", "Bash", true},
		{"Bash", "Read", false},
		{"*", "Anything", true},
		{"Read*", "ReadFile", true},
		{"Read*", "Write", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.name, func(t *testing.T) {
			m := policy.ToolName(tt.pattern)
			if got := m.Match(req(tt.name)); got != tt.want {
				t.Errorf("ToolName(%q).Match(%q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestToolNamesMatcher(t *testing.T) {
	t.Parallel()
	m := policy.ToolNames("Read", "Write")
	if !m.Match(req("Read")) {
		t.Error("expected Read to match")
	}
	if !m.Match(req("Write")) {
		t.Error("expected Write to match")
	}
	if m.Match(req("Bash")) {
		t.Error("expected Bash not to match")
	}
}

func TestAllMatcher(t *testing.T) {
	t.Parallel()
	m := policy.All(policy.ToolName("Bash"), policy.MatcherFunc(func(r policy.ToolRequest) bool {
		return len(r.Input) > 0
	}))
	if m.Match(req("Bash")) {
		t.Error("expected no match when input is empty")
	}
	r := policy.ToolRequest{Tool: stubTool{name: "Bash"}, Input: json.RawMessage(`{"cmd":"ls"}`)}
	if !m.Match(r) {
		t.Error("expected match when both conditions met")
	}
}

func TestAnyMatcher(t *testing.T) {
	t.Parallel()
	m := policy.Any(policy.ToolName("Read"), policy.ToolName("Write"))
	if !m.Match(req("Read")) {
		t.Error("expected Read to match")
	}
	if m.Match(req("Bash")) {
		t.Error("expected Bash not to match")
	}
}

func TestNotMatcher(t *testing.T) {
	t.Parallel()
	m := policy.Not(policy.ToolName("Bash"))
	if m.Match(req("Bash")) {
		t.Error("expected Bash not to match")
	}
	if !m.Match(req("Read")) {
		t.Error("expected Read to match")
	}
}

func TestRuleSetFirstMatchWins(t *testing.T) {
	t.Parallel()
	rs := policy.NewRuleSet([]policy.Rule{
		{Matcher: policy.ToolName("Bash"), Action: policy.Deny, Reason: "no bash"},
		{Matcher: policy.ToolName("*"), Action: policy.Allow},
	})
	ctx := context.Background()

	d, err := rs.Check(ctx, req("Bash"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != policy.Deny || d.Reason != "no bash" {
		t.Errorf("got %+v, want Deny with reason", d)
	}

	d, err = rs.Check(ctx, req("Read"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != policy.Allow {
		t.Errorf("got %+v, want Allow", d)
	}
}

func TestRuleSetFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rs := policy.NewRuleSet([]policy.Rule{
		{Matcher: policy.ToolName("Read"), Action: policy.Allow},
	})
	d, _ := rs.Check(ctx, req("Bash"))
	if d.Action != policy.Ask {
		t.Errorf("default fallback should be Ask, got %v", d.Action)
	}

	rs = policy.NewRuleSet([]policy.Rule{
		{Matcher: policy.ToolName("Read"), Action: policy.Allow},
	}, policy.WithFallback(policy.Deny))
	d, _ = rs.Check(ctx, req("Bash"))
	if d.Action != policy.Deny {
		t.Errorf("custom fallback should be Deny, got %v", d.Action)
	}
}

func TestChainShortCircuits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	called := false
	p := policy.Chain(
		policy.DenyAll(),
		policy.PolicyFunc(func(_ context.Context, _ policy.ToolRequest) (policy.Decision, error) {
			called = true
			return policy.Decision{Action: policy.Allow}, nil
		}),
	)
	d, _ := p.Check(ctx, req("Bash"))
	if d.Action != policy.Deny {
		t.Errorf("expected Deny, got %v", d.Action)
	}
	if called {
		t.Error("second policy should not be called after Deny")
	}
}

func TestChainAllowContinues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	p := policy.Chain(
		policy.AllowAll(),
		policy.FromRules([]policy.Rule{
			{Matcher: policy.ToolName("Bash"), Action: policy.Deny, Reason: "blocked"},
		}, policy.WithFallback(policy.Allow)),
	)
	d, _ := p.Check(ctx, req("Bash"))
	if d.Action != policy.Deny {
		t.Errorf("expected Deny from second policy, got %v", d.Action)
	}
	d, _ = p.Check(ctx, req("Read"))
	if d.Action != policy.Allow {
		t.Errorf("expected Allow, got %v", d.Action)
	}
}

func TestChainModify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	modifier := policy.PolicyFunc(func(_ context.Context, r policy.ToolRequest) (policy.Decision, error) {
		return policy.Decision{
			Action:   policy.Modify,
			Modified: &policy.ToolRequest{Tool: r.Tool, Input: json.RawMessage(`{"modified":true}`)},
		}, nil
	})
	checker := policy.PolicyFunc(func(_ context.Context, r policy.ToolRequest) (policy.Decision, error) {
		if string(r.Input) == `{"modified":true}` {
			return policy.Decision{Action: policy.Allow}, nil
		}
		return policy.Decision{Action: policy.Deny, Reason: "not modified"}, nil
	})

	p := policy.Chain(modifier, checker)
	d, _ := p.Check(ctx, req("Bash"))
	if d.Action != policy.Allow {
		t.Errorf("expected Allow after modify, got %v", d.Action)
	}
}

func TestFromRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	p := policy.FromRules([]policy.Rule{
		{Matcher: policy.ToolName("Bash"), Action: policy.Deny, Reason: "no bash"},
	})
	d, _ := p.Check(ctx, req("Bash"))
	if d.Action != policy.Deny {
		t.Errorf("expected Deny, got %v", d.Action)
	}
}

func TestInputMatcher(t *testing.T) {
	t.Parallel()
	m := policy.Input("file_path", policy.HasPrefix("/etc/"))

	r := policy.ToolRequest{
		Tool:  stubTool{name: "Read"},
		Input: json.RawMessage(`{"file_path":"/etc/passwd"}`),
	}
	if !m.Match(r) {
		t.Error("expected /etc/passwd to match HasPrefix(/etc/)")
	}

	r.Input = json.RawMessage(`{"file_path":"/home/user/file.txt"}`)
	if m.Match(r) {
		t.Error("expected /home/user/file.txt not to match")
	}
}

func TestInputMatcherMissingField(t *testing.T) {
	t.Parallel()
	m := policy.Input("file_path", policy.HasPrefix("/"))

	r := policy.ToolRequest{
		Tool:  stubTool{name: "Bash"},
		Input: json.RawMessage(`{"command":"ls"}`),
	}
	if m.Match(r) {
		t.Error("expected no match when field is missing")
	}
}

func TestInputMatcherEmptyInput(t *testing.T) {
	t.Parallel()
	m := policy.Input("file_path", policy.HasPrefix("/"))
	if m.Match(req("Read")) {
		t.Error("expected no match on empty input")
	}
}

func TestPredicates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pred policy.Predicate
		val  string
		want bool
	}{
		{"HasPrefix match", policy.HasPrefix("/etc"), "/etc/passwd", true},
		{"HasPrefix no match", policy.HasPrefix("/etc"), "/home/user", false},
		{"HasSuffix match", policy.HasSuffix(".env"), "app/.env", true},
		{"HasSuffix no match", policy.HasSuffix(".env"), "app/.envrc", false},
		{"Contains match", policy.Contains("secret"), "/path/secrets/key", true},
		{"Contains no match", policy.Contains("secret"), "/path/public/key", false},
		{"Equals match", policy.Equals("exact"), "exact", true},
		{"Equals no match", policy.Equals("exact"), "other", false},
		{"Glob match", policy.Glob("*.env"), "app.env", true},
		{"Glob no match", policy.Glob("*.env"), "app.envrc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pred(tt.val); got != tt.want {
				t.Errorf("%s(%q) = %v, want %v", tt.name, tt.val, got, tt.want)
			}
		})
	}
}

func TestAnyOfPredicate(t *testing.T) {
	t.Parallel()
	pred := policy.AnyOf(
		policy.HasSuffix(".env"),
		policy.HasPrefix(".git/"),
		policy.Contains("credentials"),
	)
	if !pred("app/.env") {
		t.Error("expected .env to match")
	}
	if !pred(".git/config") {
		t.Error("expected .git/ to match")
	}
	if !pred("/path/credentials.json") {
		t.Error("expected credentials to match")
	}
	if pred("/path/normal.go") {
		t.Error("expected normal.go not to match")
	}
}

func TestInputWithRuleSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	p := policy.FromRules([]policy.Rule{
		{
			Matcher: policy.All(
				policy.ToolNames("Read", "Write", "Edit"),
				policy.Input("file_path", policy.AnyOf(
					policy.HasSuffix(".env"),
					policy.HasPrefix("/etc/"),
				)),
			),
			Action: policy.Ask,
			Reason: "sensitive file",
		},
		{Matcher: policy.ToolName("*"), Action: policy.Allow},
	})

	d, _ := p.Check(ctx, policy.ToolRequest{
		Tool:  stubTool{name: "Read"},
		Input: json.RawMessage(`{"file_path":"/etc/shadow"}`),
	})
	if d.Action != policy.Ask {
		t.Errorf("expected Ask for /etc/shadow, got %v", d.Action)
	}

	d, _ = p.Check(ctx, policy.ToolRequest{
		Tool:  stubTool{name: "Write"},
		Input: json.RawMessage(`{"file_path":"app/.env"}`),
	})
	if d.Action != policy.Ask {
		t.Errorf("expected Ask for .env, got %v", d.Action)
	}

	d, _ = p.Check(ctx, policy.ToolRequest{
		Tool:  stubTool{name: "Read"},
		Input: json.RawMessage(`{"file_path":"/home/user/main.go"}`),
	})
	if d.Action != policy.Allow {
		t.Errorf("expected Allow for normal file, got %v", d.Action)
	}
}
