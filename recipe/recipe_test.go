package recipe

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// mockModel implements blades.ModelProvider for testing.
type mockModel struct {
	name string
}

func (m *mockModel) Name() string { return m.name }
func (m *mockModel) Generate(_ context.Context, _ *blades.ModelRequest) (*blades.ModelResponse, error) {
	return &blades.ModelResponse{
		Message: &blades.Message{Role: blades.RoleAssistant, Status: blades.StatusCompleted},
	}, nil
}
func (m *mockModel) NewStreaming(_ context.Context, _ *blades.ModelRequest) iter.Seq2[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {}
}

type captureRequestModel struct {
	name        string
	response    string
	messages    []*blades.Message
	instruction string
	toolNames   []string
}

func (m *captureRequestModel) Name() string { return m.name }

func (m *captureRequestModel) Generate(_ context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	m.messages = append(m.messages[:0], req.Messages...)
	m.toolNames = m.toolNames[:0]
	for _, tool := range req.Tools {
		m.toolNames = append(m.toolNames, tool.Name())
	}
	if req.Instruction != nil {
		m.instruction = req.Instruction.Text()
	}
	msg := blades.NewAssistantMessage(blades.StatusCompleted)
	text := m.response
	if text == "" {
		text = "ok"
	}
	msg.Parts = append(msg.Parts, blades.TextPart{Text: text})
	return &blades.ModelResponse{Message: msg}, nil
}

func (m *captureRequestModel) NewStreaming(_ context.Context, _ *blades.ModelRequest) iter.Seq2[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {}
}

// mockTool implements tools.Tool for testing.
type mockTool struct {
	name string
}

func (t *mockTool) Name() string                                       { return t.name }
func (t *mockTool) Description() string                                { return "mock tool" }
func (t *mockTool) InputSchema() *jsonschema.Schema                    { return nil }
func (t *mockTool) OutputSchema() *jsonschema.Schema                   { return nil }
func (t *mockTool) Handle(_ context.Context, _ string) (string, error) { return "ok", nil }

func newTestModelRegistry() *Registry {
	r := NewRegistry()
	r.Register("gpt-4o", &mockModel{name: "gpt-4o"})
	r.Register("gpt-4o-mini", &mockModel{name: "gpt-4o-mini"})
	r.Register("claude-sonnet", &mockModel{name: "claude-sonnet"})
	return r
}

func newTestToolRegistry() *StaticToolRegistry {
	r := NewStaticToolRegistry()
	r.Register("web-search", &mockTool{name: "web-search"})
	return r
}

// --- Parse / Load Tests ---

func TestParseBasicYAML(t *testing.T) {
	spec, err := LoadFromFile("testdata/basic.yaml")
	if err != nil {
		t.Fatalf("failed to parse basic.yaml: %v", err)
	}
	if spec.Name != "code-reviewer" {
		t.Errorf("expected name %q, got %q", "code-reviewer", spec.Name)
	}
	if spec.Model != "gpt-4o" {
		t.Errorf("expected model %q, got %q", "gpt-4o", spec.Model)
	}
	if len(spec.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(spec.Parameters))
	}
	if spec.Parameters[0].Type != ParameterSelect {
		t.Errorf("expected parameter type %q, got %q", ParameterSelect, spec.Parameters[0].Type)
	}
	if len(spec.Parameters[0].Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(spec.Parameters[0].Options))
	}
}

func TestParseSequentialYAML(t *testing.T) {
	spec, err := LoadFromFile("testdata/sequential.yaml")
	if err != nil {
		t.Fatalf("failed to parse sequential.yaml: %v", err)
	}
	if spec.Execution != ExecutionSequential {
		t.Errorf("expected execution %q, got %q", ExecutionSequential, spec.Execution)
	}
	if len(spec.SubRecipes) != 2 {
		t.Fatalf("expected 2 sub_recipes, got %d", len(spec.SubRecipes))
	}
	if spec.SubRecipes[0].OutputKey != "syntax_report" {
		t.Errorf("expected output_key %q, got %q", "syntax_report", spec.SubRecipes[0].OutputKey)
	}
}

func TestParseToolYAML(t *testing.T) {
	spec, err := LoadFromFile("testdata/tool.yaml")
	if err != nil {
		t.Fatalf("failed to parse tool.yaml: %v", err)
	}
	if spec.Execution != ExecutionTool {
		t.Errorf("expected execution %q, got %q", ExecutionTool, spec.Execution)
	}
	if len(spec.SubRecipes) != 2 {
		t.Fatalf("expected 2 sub_recipes, got %d", len(spec.SubRecipes))
	}
}

func TestParseParallelYAML(t *testing.T) {
	spec, err := LoadFromFile("testdata/parallel.yaml")
	if err != nil {
		t.Fatalf("failed to parse parallel.yaml: %v", err)
	}
	if spec.Execution != ExecutionParallel {
		t.Errorf("expected execution %q, got %q", ExecutionParallel, spec.Execution)
	}
}

func TestLoadFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"recipe.yaml": {
			Data: []byte(`version: "1.0"
name: from-fs
model: gpt-4o
instruction: test instruction`),
		},
	}
	spec, err := LoadFromFS(fsys, "recipe.yaml")
	if err != nil {
		t.Fatalf("failed to load from fs: %v", err)
	}
	if spec.Name != "from-fs" {
		t.Fatalf("unexpected name: %q", spec.Name)
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte(": not-valid-yaml"))
	if err == nil {
		t.Fatal("expected parse error for invalid YAML")
	}
}

// --- Validation Tests ---

func TestValidateNoName(t *testing.T) {
	_, err := LoadFromFile("testdata/invalid_no_name.yaml")
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
}

func TestValidateNoExecution(t *testing.T) {
	_, err := LoadFromFile("testdata/invalid_no_execution.yaml")
	if err == nil {
		t.Fatal("expected validation error for missing execution with sub_recipes")
	}
}

func TestValidateSelectNoOptions(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "test",
		Model:       "gpt-4o",
		Instruction: "test",
		Parameters: []ParameterSpec{
			{Name: "choice", Type: ParameterSelect, Description: "pick one"},
		},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected validation error for select without options")
	}
}

func TestValidateDuplicateParameter(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "test",
		Model:       "gpt-4o",
		Instruction: "test",
		Parameters: []ParameterSpec{
			{Name: "lang", Type: ParameterString, Description: "a"},
			{Name: "lang", Type: ParameterString, Description: "b"},
		},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected validation error for duplicate parameter")
	}
}

func TestValidateParams(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "test",
		Model:       "gpt-4o",
		Instruction: "test",
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterSelect, Description: "lang", Required: ParameterRequired, Options: []string{"go", "python"}},
		},
	}
	// Missing required param
	if err := ValidateParams(spec, map[string]any{}); err == nil {
		t.Fatal("expected error for missing required parameter")
	}
	// Invalid select value
	if err := ValidateParams(spec, map[string]any{"language": "rust"}); err == nil {
		t.Fatal("expected error for invalid select value")
	}
	// Valid
	if err := ValidateParams(spec, map[string]any{"language": "go"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Template Tests ---

func TestRenderTemplate(t *testing.T) {
	result, err := renderTemplate("Hello {{.name}}, you code in {{.lang}}.", map[string]any{
		"name": "Alice",
		"lang": "Go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello Alice, you code in Go." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRenderTemplateEmpty(t *testing.T) {
	result, err := renderTemplate("", map[string]any{"x": "y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestRenderTemplatePreservingUnknown(t *testing.T) {
	result, err := renderTemplatePreservingUnknown("lang={{.lang}}, report={{.report}}", map[string]any{
		"lang": "go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "lang=go, report={{.report}}" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestRenderTemplatePreservingUnknownMultiple(t *testing.T) {
	result, err := renderTemplatePreservingUnknown("{{.a}}/{{.b}}/{{.c}}", map[string]any{
		"a": "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "x/{{.b}}/{{.c}}" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestResolveParams(t *testing.T) {
	params := []ParameterSpec{
		{Name: "language", Type: ParameterString, Default: "go"},
		{Name: "style", Type: ParameterString, Default: "thorough"},
	}
	merged := resolveParams(params, map[string]any{"language": "python"})
	if merged["language"] != "python" {
		t.Errorf("expected user override, got %v", merged["language"])
	}
	if merged["style"] != "thorough" {
		t.Errorf("expected default, got %v", merged["style"])
	}
}

func TestHasTemplateActions(t *testing.T) {
	if !hasTemplateActions("Hello {{.name}}") {
		t.Error("expected true for template with actions")
	}
	if hasTemplateActions("Hello world") {
		t.Error("expected false for plain string")
	}
}

// --- Registry Tests ---

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register("test-model", &mockModel{name: "test-model"})

	model, err := r.Resolve("test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.Name() != "test-model" {
		t.Errorf("expected %q, got %q", "test-model", model.Name())
	}

	_, err = r.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent model")
	}
}

func TestStaticToolRegistry(t *testing.T) {
	r := NewStaticToolRegistry()
	r.Register("my-tool", &mockTool{name: "my-tool"})

	tool, err := r.Resolve("my-tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Name() != "my-tool" {
		t.Errorf("expected %q, got %q", "my-tool", tool.Name())
	}

	_, err = r.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

// --- Build Tests ---

func TestBuildBasicAgent(t *testing.T) {
	spec, err := LoadFromFile("testdata/basic.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent.Name() != "code-reviewer" {
		t.Errorf("expected name %q, got %q", "code-reviewer", agent.Name())
	}
}

func TestBuildSequentialAgent(t *testing.T) {
	spec, err := LoadFromFile("testdata/sequential.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent.Name() != "code-review-pipeline" {
		t.Errorf("expected name %q, got %q", "code-review-pipeline", agent.Name())
	}
}

func TestBuildParallelAgent(t *testing.T) {
	spec, err := LoadFromFile("testdata/parallel.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent.Name() != "multi-review" {
		t.Errorf("expected name %q, got %q", "multi-review", agent.Name())
	}
}

func TestBuildToolAgent(t *testing.T) {
	spec, err := LoadFromFile("testdata/tool.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"topic": "AI safety"}),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent.Name() != "research-assistant" {
		t.Errorf("expected name %q, got %q", "research-assistant", agent.Name())
	}
}

func TestBuildWithDefaults(t *testing.T) {
	spec, err := LoadFromFile("testdata/defaults.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	// Build without providing any params - defaults should kick in
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent.Name() != "with-defaults" {
		t.Errorf("expected name %q, got %q", "with-defaults", agent.Name())
	}
}

func TestBuildWithReferencedToolsRequiresToolRegistry(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "with-tools",
		Model:       "gpt-4o",
		Instruction: "use tools",
		Tools:       []string{"web-search"},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error when tools are referenced without tool registry")
	}
}

func TestBuildResolvesExternalTools(t *testing.T) {
	model := &captureRequestModel{name: "openai"}
	modelRegistry := NewRegistry()
	modelRegistry.Register("openai", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "with-tools",
		Model:       "openai",
		Instruction: "use tools",
		Tools:       []string{"web-search"},
	}
	agent, err := Build(spec,
		WithModelRegistry(modelRegistry),
		WithToolRegistry(newTestToolRegistry()),
	)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("search now")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !slices.Contains(model.toolNames, "web-search") {
		t.Fatalf("expected tool web-search in request, got %v", model.toolNames)
	}
}

func TestBuildMissingModelRegistry(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "test",
		Model:       "gpt-4o",
		Instruction: "test",
	}
	_, err := Build(spec)
	if err == nil {
		t.Fatal("expected error for missing model registry")
	}
}

func TestBuildMissingRequiredParam(t *testing.T) {
	spec, err := LoadFromFile("testdata/basic.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	// Don't provide required "language" param
	_, err = Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
}

func TestBuildInvalidSelectParam(t *testing.T) {
	spec, err := LoadFromFile("testdata/basic.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	_, err = Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"language": "rust"}), // not in options
	)
	if err == nil {
		t.Fatal("expected error for invalid select value")
	}
}

func TestBuildToolModeIncludesSubRecipesAndExternalTools(t *testing.T) {
	model := &captureRequestModel{name: "openai"}
	modelRegistry := NewRegistry()
	modelRegistry.Register("openai", model)
	modelRegistry.Register("worker-model", &mockModel{name: "worker-model"})

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "dispatcher",
		Model:       "openai",
		Instruction: "dispatch",
		Execution:   ExecutionTool,
		Tools:       []string{"web-search"},
		SubRecipes: []SubRecipeSpec{
			{Name: "fact-checker", Model: "worker-model", Instruction: "check"},
			{Name: "data-analyst", Model: "worker-model", Instruction: "analyze"},
		},
	}
	agent, err := Build(spec,
		WithModelRegistry(modelRegistry),
		WithToolRegistry(newTestToolRegistry()),
	)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("start")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	for _, name := range []string{"fact-checker", "data-analyst", "web-search"} {
		if !slices.Contains(model.toolNames, name) {
			t.Fatalf("expected tool %q in request, got %v", name, model.toolNames)
		}
	}
}

func TestBuildSubRecipeInheritsModel(t *testing.T) {
	// quality-reviewer in sequential.yaml has no model, should inherit gpt-4o from parent
	spec, err := LoadFromFile("testdata/sequential.yaml")
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	agent, err := Build(spec,
		WithModelRegistry(newTestModelRegistry()),
		WithParams(map[string]any{"language": "go"}),
	)
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestBuildNilSpec(t *testing.T) {
	_, err := Build(nil, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestBuildInjectsPromptAsFirstUserMessage(t *testing.T) {
	model := &captureRequestModel{name: "openai"}
	registry := NewRegistry()
	registry.Register("openai", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "prompted",
		Model:       "openai",
		Instruction: "You are helpful.",
		Prompt:      "Please review {{.language}} code first.",
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterString, Required: ParameterRequired},
		},
	}

	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"language": "go"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	runner := blades.NewRunner(agent)
	if _, err := runner.Run(context.Background(), blades.UserMessage("And then suggest improvements.")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(model.messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(model.messages))
	}
	if got := model.messages[0].Text(); got != "Please review go code first." {
		t.Fatalf("unexpected first message: %q", got)
	}
	if got := model.messages[1].Text(); got != "And then suggest improvements." {
		t.Fatalf("unexpected second message: %q", got)
	}
}

func TestBuildInjectsSubRecipePromptAsFirstUserMessage(t *testing.T) {
	model := &captureRequestModel{name: "m1"}
	registry := NewRegistry()
	registry.Register("m1", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "pipeline",
		Model:       "m1",
		Instruction: "pipeline",
		Execution:   ExecutionSequential,
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterString, Required: ParameterRequired},
		},
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "step-1",
				Instruction: "check code",
				Prompt:      "First focus on {{.language}} code style.",
			},
		},
	}

	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"language": "go"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	runner := blades.NewRunner(agent)
	if _, err := runner.Run(context.Background(), blades.UserMessage("Then suggest improvements.")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(model.messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(model.messages))
	}
	if got := model.messages[0].Text(); got != "First focus on go code style." {
		t.Fatalf("unexpected first message: %q", got)
	}
	if got := model.messages[1].Text(); got != "Then suggest improvements." {
		t.Fatalf("unexpected second message: %q", got)
	}
}

func TestBuildFailsWhenPromptRenderHasMissingParam(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "bad-prompt",
		Model:       "gpt-4o",
		Instruction: "instruction",
		Prompt:      "missing={{.unknown}}",
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected prompt render error")
	}
}

func TestBuildFailsWhenSubRecipePromptRenderHasMissingParam(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "bad-sub-prompt",
		Model:       "gpt-4o",
		Instruction: "instruction",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "step-1",
				Instruction: "check",
				Prompt:      "missing={{.unknown}}",
			},
		},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected sub-recipe prompt render error")
	}
}

func TestBuildSequentialRendersParamsAndPreservesOutputTemplate(t *testing.T) {
	first := &captureRequestModel{name: "m1", response: "syntax-ok"}
	second := &captureRequestModel{name: "m2", response: "done"}
	registry := NewRegistry()
	registry.Register("m1", first)
	registry.Register("m2", second)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "pipeline",
		Model:       "m1",
		Instruction: "pipeline",
		Execution:   ExecutionSequential,
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterString, Required: ParameterRequired},
		},
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "step-1",
				Model:       "m1",
				Instruction: "check {{.language}} syntax",
				OutputKey:   "syntax_report",
			},
			{
				Name:        "step-2",
				Model:       "m2",
				Instruction: "lang={{.language}}; report={{.syntax_report}}",
			},
		},
	}

	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"language": "go"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	runner := blades.NewRunner(agent)
	if _, err := runner.Run(context.Background(), blades.UserMessage("run")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(second.instruction, "lang=go; report=syntax-ok") {
		t.Fatalf("unexpected second instruction: %q", second.instruction)
	}
}

func TestBuildValidatesSubRecipeParams(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "pipeline",
		Model:       "gpt-4o",
		Instruction: "pipeline",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "step-1",
				Instruction: "style={{.style}}",
				Parameters: []ParameterSpec{
					{Name: "style", Type: ParameterString, Required: ParameterRequired},
				},
			},
		},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error for missing required sub-recipe param")
	}
}

func TestValidateRejectsOutputKeyInToolMode(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "tool-mode",
		Model:       "gpt-4o",
		Instruction: "route",
		Execution:   ExecutionTool,
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "worker",
				Instruction: "do work",
				OutputKey:   "worker_output",
			},
		},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected validation error for output_key in tool mode")
	}
}

// --- Additional Validation Tests ---

func TestValidateNilSpec(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestValidateNoVersion(t *testing.T) {
	spec := &RecipeSpec{Name: "x", Model: "m", Instruction: "i"}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("error should mention version: %v", err)
	}
}

func TestValidateNoInstruction(t *testing.T) {
	spec := &RecipeSpec{Version: "1.0", Name: "x", Model: "m"}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for missing instruction")
	}
	if !strings.Contains(err.Error(), "instruction") {
		t.Fatalf("error should mention instruction: %v", err)
	}
}

func TestValidateNoModelWithoutSubRecipes(t *testing.T) {
	spec := &RecipeSpec{Version: "1.0", Name: "x", Instruction: "i"}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for missing model without sub_recipes")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error should mention model: %v", err)
	}
}

func TestValidateInvalidExecutionMode(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Execution:   "invalid-mode",
	}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for invalid execution mode")
	}
	if !strings.Contains(err.Error(), "invalid-mode") {
		t.Fatalf("error should mention invalid mode: %v", err)
	}
}

func TestValidateParameterNoType(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Parameters:  []ParameterSpec{{Name: "p", Description: "d"}},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected error for parameter without type")
	}
}

func TestValidateParameterInvalidType(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Parameters:  []ParameterSpec{{Name: "p", Type: "float", Description: "d"}},
	}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for invalid parameter type")
	}
	if !strings.Contains(err.Error(), "float") {
		t.Fatalf("error should mention type: %v", err)
	}
}

func TestValidateParameterNoName(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Parameters:  []ParameterSpec{{Type: ParameterString, Description: "d"}},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected error for parameter without name")
	}
}

func TestValidateSubRecipeNoName(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes:  []SubRecipeSpec{{Instruction: "sub"}},
	}
	err := Validate(spec)
	if err == nil {
		t.Fatal("expected error for sub_recipe without name")
	}
	if !strings.Contains(err.Error(), "sub_recipe[0]") {
		t.Fatalf("error should mention index: %v", err)
	}
}

func TestValidateSubRecipeNoInstruction(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes:  []SubRecipeSpec{{Name: "s"}},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected error for sub_recipe without instruction")
	}
}

func TestValidateSubRecipeDuplicateParam(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{{
			Name:        "s",
			Instruction: "do",
			Parameters: []ParameterSpec{
				{Name: "a", Type: ParameterString},
				{Name: "a", Type: ParameterString},
			},
		}},
	}
	if err := Validate(spec); err == nil {
		t.Fatal("expected error for duplicate sub_recipe parameter")
	}
}

func TestValidateParamsNilSpec(t *testing.T) {
	if err := ValidateParams(nil, map[string]any{}); err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestValidateParamsSelectNonString(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Parameters:  []ParameterSpec{{Name: "p", Type: ParameterSelect, Options: []string{"a", "b"}, Required: ParameterRequired}},
	}
	err := ValidateParams(spec, map[string]any{"p": 123})
	if err == nil {
		t.Fatal("expected error for non-string select value")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("error should mention type: %v", err)
	}
}

func TestValidateParamsOptionalMissing(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Parameters:  []ParameterSpec{{Name: "p", Type: ParameterString, Required: ParameterOptional}},
	}
	if err := ValidateParams(spec, map[string]any{}); err != nil {
		t.Fatalf("optional param should not cause error: %v", err)
	}
}

func TestValidateAcceptsSequentialParallelTool(t *testing.T) {
	for _, mode := range []ExecutionMode{ExecutionSequential, ExecutionParallel, ExecutionTool} {
		spec := &RecipeSpec{
			Version:     "1.0",
			Name:        "x",
			Model:       "m",
			Instruction: "i",
			Execution:   mode,
			SubRecipes:  []SubRecipeSpec{{Name: "s", Instruction: "do"}},
		}
		if err := Validate(spec); err != nil {
			t.Errorf("mode %q should be valid: %v", mode, err)
		}
	}
}

// --- Additional Template Tests ---

func TestRenderTemplatePreservingUnknownAllKnown(t *testing.T) {
	result, err := renderTemplatePreservingUnknown("{{.a}}-{{.b}}", map[string]any{
		"a": "x", "b": "y",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "x-y" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestRenderTemplatePreservingUnknownEmpty(t *testing.T) {
	result, err := renderTemplatePreservingUnknown("", map[string]any{"a": "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestRenderTemplatePreservingUnknownNoKeys(t *testing.T) {
	result, err := renderTemplatePreservingUnknown("plain text", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "plain text" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestRenderTemplateSyntaxError(t *testing.T) {
	_, err := renderTemplate("{{.bad", map[string]any{})
	if err == nil {
		t.Fatal("expected error for malformed template")
	}
}

func TestRenderTemplateMissingKeyStrict(t *testing.T) {
	_, err := renderTemplate("{{.missing}}", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing key in strict mode")
	}
}

func TestExtractMissingKeyNil(t *testing.T) {
	key, ok := extractMissingKey(nil)
	if ok || key != "" {
		t.Fatal("expected empty result for nil error")
	}
}

func TestExtractMissingKeyNonMatching(t *testing.T) {
	key, ok := extractMissingKey(fmt.Errorf("some other error"))
	if ok || key != "" {
		t.Fatal("expected empty result for non-matching error")
	}
}

func TestExtractMissingKeyMatching(t *testing.T) {
	key, ok := extractMissingKey(fmt.Errorf(`template: recipe:1:2: executing "recipe" at <.report>: map has no entry for key "report"`))
	if !ok {
		t.Fatal("expected match")
	}
	if key != "report" {
		t.Fatalf("expected key %q, got %q", "report", key)
	}
}

func TestCloneMapNil(t *testing.T) {
	out := cloneMap(nil)
	if out == nil {
		t.Fatal("expected non-nil map")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(out))
	}
}

func TestCloneMapDoesNotMutateOriginal(t *testing.T) {
	src := map[string]any{"a": "1"}
	out := cloneMap(src)
	out["b"] = "2"
	if _, ok := src["b"]; ok {
		t.Fatal("clone mutated the original")
	}
}

func TestResolveParamsNilUser(t *testing.T) {
	specs := []ParameterSpec{
		{Name: "a", Type: ParameterString, Default: "x"},
	}
	merged := resolveParams(specs, nil)
	if merged["a"] != "x" {
		t.Fatalf("expected default, got %v", merged["a"])
	}
}

func TestResolveParamsNoDefaults(t *testing.T) {
	specs := []ParameterSpec{
		{Name: "a", Type: ParameterString},
	}
	merged := resolveParams(specs, map[string]any{"a": "y"})
	if merged["a"] != "y" {
		t.Fatalf("expected user value, got %v", merged["a"])
	}
}

func TestResolveParamsExtraUserKeysPassthrough(t *testing.T) {
	specs := []ParameterSpec{}
	merged := resolveParams(specs, map[string]any{"extra": "val"})
	if merged["extra"] != "val" {
		t.Fatalf("expected passthrough, got %v", merged["extra"])
	}
}

// --- Additional Loader Tests ---

func TestLoadFromFileNonexistent(t *testing.T) {
	_, err := LoadFromFile("testdata/does_not_exist.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadFromFSNonexistent(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := LoadFromFS(fsys, "missing.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestLoadFromFSInvalidContent(t *testing.T) {
	fsys := fstest.MapFS{
		"bad.yaml": {Data: []byte("not: [valid: recipe}")},
	}
	_, err := LoadFromFS(fsys, "bad.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML content")
	}
}

// --- Additional Registry Tests ---

func TestRegistryOverwrite(t *testing.T) {
	r := NewRegistry()
	r.Register("m", &mockModel{name: "v1"})
	r.Register("m", &mockModel{name: "v2"})
	m, err := r.Resolve("m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name() != "v2" {
		t.Fatalf("expected overwritten value, got %q", m.Name())
	}
}

func TestStaticToolRegistryOverwrite(t *testing.T) {
	r := NewStaticToolRegistry()
	r.Register("t", &mockTool{name: "v1"})
	r.Register("t", &mockTool{name: "v2"})
	tool, err := r.Resolve("t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Name() != "v2" {
		t.Fatalf("expected overwritten value, got %q", tool.Name())
	}
}

// --- Additional Build / Error Path Tests ---

func TestBuildUnresolvableModel(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "nonexistent-model",
		Instruction: "i",
	}
	_, err := Build(spec, WithModelRegistry(NewRegistry()))
	if err == nil {
		t.Fatal("expected error for unresolvable model")
	}
	if !strings.Contains(err.Error(), "nonexistent-model") {
		t.Fatalf("error should mention model name: %v", err)
	}
}

func TestBuildSubRecipeNoModelAndNoParent(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{Name: "s", Instruction: "do"},
		},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error when sub_recipe has no model and parent has no model")
	}
	if !strings.Contains(err.Error(), "no model") {
		t.Fatalf("error should mention model: %v", err)
	}
}

func TestBuildSubRecipeUnresolvableModel(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "gpt-4o",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{Name: "s", Model: "nonexistent", Instruction: "do"},
		},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err == nil {
		t.Fatal("expected error for unresolvable sub_recipe model")
	}
}

func TestBuildSubRecipeUnresolvableTool(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "gpt-4o",
		Instruction: "i",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{Name: "s", Instruction: "do", Tools: []string{"missing-tool"}},
		},
	}
	_, err := Build(spec, WithModelRegistry(newTestModelRegistry()), WithToolRegistry(NewStaticToolRegistry()))
	if err == nil {
		t.Fatal("expected error for unresolvable sub_recipe tool")
	}
}

func TestBuildSingleAgentVerifiesInstruction(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "reviewer",
		Model:       "m",
		Instruction: "Review {{.language}} code carefully.",
		Parameters:  []ParameterSpec{{Name: "language", Type: ParameterString, Required: ParameterRequired}},
	}
	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"language": "python"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("check")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(model.instruction, "Review python code carefully.") {
		t.Fatalf("instruction not rendered: %q", model.instruction)
	}
}

func TestBuildSingleAgentDescription(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Description: "my description",
		Model:       "gpt-4o",
		Instruction: "i",
	}
	agent, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if agent.Description() != "my description" {
		t.Fatalf("expected description %q, got %q", "my description", agent.Description())
	}
}

func TestBuildSingleAgentMaxIterations(t *testing.T) {
	// MaxIterations is not directly observable, but we can confirm
	// the build succeeds with it set.
	spec := &RecipeSpec{
		Version:       "1.0",
		Name:          "x",
		Model:         "gpt-4o",
		Instruction:   "i",
		MaxIterations: 5,
	}
	agent, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestBuildSingleAgentOutputKey(t *testing.T) {
	model := &captureRequestModel{name: "m", response: "hello world"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		OutputKey:   "result",
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	session := blades.NewSession()
	runner := blades.NewRunner(agent)
	if _, err := runner.Run(context.Background(), blades.UserMessage("go"), blades.WithSession(session)); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if val, ok := session.State()["result"]; !ok || val != "hello world" {
		t.Fatalf("expected output_key in session state, got %v", session.State())
	}
}

func TestBuildParallelAgentRunsSubAgents(t *testing.T) {
	registry := NewRegistry()
	registry.Register("m1", &mockModel{name: "m1"})
	registry.Register("m2", &mockModel{name: "m2"})

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "par",
		Model:       "m1",
		Instruction: "parallel run",
		Execution:   ExecutionParallel,
		SubRecipes: []SubRecipeSpec{
			{Name: "a", Model: "m1", Instruction: "do a", OutputKey: "out_a"},
			{Name: "b", Model: "m2", Instruction: "do b", OutputKey: "out_b"},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	session := blades.NewSession()
	runner := blades.NewRunner(agent)
	msg, err := runner.Run(context.Background(), blades.UserMessage("go"), blades.WithSession(session))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if msg == nil {
		t.Fatal("expected non-nil message from parallel run")
	}
}

func TestBuildToolAgentDescription(t *testing.T) {
	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "tool-agent",
		Description: "routes tasks",
		Model:       "gpt-4o",
		Instruction: "route tasks",
		Execution:   ExecutionTool,
		SubRecipes: []SubRecipeSpec{
			{Name: "w", Description: "worker desc", Instruction: "work"},
		},
	}
	agent, err := Build(spec, WithModelRegistry(newTestModelRegistry()))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if agent.Description() != "routes tasks" {
		t.Fatalf("expected description %q, got %q", "routes tasks", agent.Description())
	}
}

// --- Prompt Injection Tests ---

func TestPromptInjectedAgentEmptyPrompt(t *testing.T) {
	// When prompt is empty, the agent should pass through directly
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	base, err := blades.NewAgent("base", blades.WithModel(model), blades.WithInstruction("i"))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	wrapped, err := withPromptTemplate(base, "test", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With empty prompt, should return the same agent
	if wrapped != base {
		t.Fatal("expected same agent when prompt is empty")
	}
}

func TestPromptInjectedAgentNoPrompt(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		// No prompt
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("hello")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	// With no prompt, only the user message should be in messages
	if len(model.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(model.messages))
	}
}

func TestPromptInjectedAgentWithNilMessage(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "x",
		Model:       "m",
		Instruction: "i",
		Prompt:      "the prompt",
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	// Invocation with nil Message - prompt becomes the Message
	gen := agent.Run(context.Background(), &blades.Invocation{})
	for _, err := range gen {
		if err != nil {
			break
		}
	}
	if len(model.messages) != 1 {
		t.Fatalf("expected prompt as message, got %d messages", len(model.messages))
	}
	if got := model.messages[0].Text(); got != "the prompt" {
		t.Fatalf("expected prompt text, got %q", got)
	}
}

// --- End-to-End Integration Tests ---

func TestE2ESingleAgentRun(t *testing.T) {
	model := &captureRequestModel{name: "m", response: "reviewed!"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "reviewer",
		Model:       "m",
		Instruction: "Review {{.lang}} code",
		Parameters: []ParameterSpec{
			{Name: "lang", Type: ParameterString, Required: ParameterRequired},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"lang": "go"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	runner := blades.NewRunner(agent)
	msg, err := runner.Run(context.Background(), blades.UserMessage("check my code"))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if msg.Text() != "reviewed!" {
		t.Fatalf("unexpected output: %q", msg.Text())
	}
	if !strings.Contains(model.instruction, "Review go code") {
		t.Fatalf("instruction not rendered: %q", model.instruction)
	}
}

func TestE2ESequentialPipelineOutputKeyPropagation(t *testing.T) {
	step1Model := &captureRequestModel{name: "s1", response: "analysis-result"}
	step2Model := &captureRequestModel{name: "s2", response: "final-summary"}
	registry := NewRegistry()
	registry.Register("s1", step1Model)
	registry.Register("s2", step2Model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "pipeline",
		Instruction: "coordinate",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{
				Name:        "analyzer",
				Model:       "s1",
				Instruction: "Analyze the input",
				OutputKey:   "analysis",
			},
			{
				Name:        "summarizer",
				Model:       "s2",
				Instruction: "Summarize based on: {{.analysis}}",
			},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	session := blades.NewSession()
	runner := blades.NewRunner(agent)
	msg, err := runner.Run(context.Background(), blades.UserMessage("start"), blades.WithSession(session))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if msg.Text() != "final-summary" {
		t.Fatalf("expected final output, got %q", msg.Text())
	}
	// Verify step2's instruction was rendered with step1's output
	if !strings.Contains(step2Model.instruction, "Summarize based on: analysis-result") {
		t.Fatalf("step2 instruction not rendered with output_key: %q", step2Model.instruction)
	}
	// Verify session state has the output_key from step1
	if val, ok := session.State()["analysis"]; !ok || val != "analysis-result" {
		t.Fatalf("expected analysis in session state, got %v", session.State())
	}
}

func TestE2EToolModeAgentRun(t *testing.T) {
	model := &captureRequestModel{name: "m", response: "dispatched"}
	workerModel := &mockModel{name: "w"}
	registry := NewRegistry()
	registry.Register("m", model)
	registry.Register("w", workerModel)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "dispatcher",
		Model:       "m",
		Instruction: "Dispatch tasks about {{.topic}}",
		Execution:   ExecutionTool,
		Parameters: []ParameterSpec{
			{Name: "topic", Type: ParameterString, Required: ParameterRequired},
		},
		SubRecipes: []SubRecipeSpec{
			{Name: "checker", Model: "w", Description: "check facts", Instruction: "verify"},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"topic": "AI"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	msg, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if msg.Text() != "dispatched" {
		t.Fatalf("unexpected: %q", msg.Text())
	}
	if !strings.Contains(model.instruction, "Dispatch tasks about AI") {
		t.Fatalf("instruction not rendered: %q", model.instruction)
	}
	if !slices.Contains(model.toolNames, "checker") {
		t.Fatalf("expected checker tool, got %v", model.toolNames)
	}
}

func TestE2EWithDefaultParamsNoUserOverride(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "defaults",
		Model:       "m",
		Instruction: "lang={{.language}}, mode={{.mode}}",
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterString, Default: "go"},
			{Name: "mode", Type: ParameterSelect, Default: "fast", Options: []string{"fast", "slow"}},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("run")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(model.instruction, "lang=go, mode=fast") {
		t.Fatalf("defaults not applied: %q", model.instruction)
	}
}

func TestE2EWithDefaultParamsPartialOverride(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "defaults",
		Model:       "m",
		Instruction: "lang={{.language}}, mode={{.mode}}",
		Parameters: []ParameterSpec{
			{Name: "language", Type: ParameterString, Default: "go"},
			{Name: "mode", Type: ParameterSelect, Default: "fast", Options: []string{"fast", "slow"}},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry), WithParams(map[string]any{"language": "python"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("run")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(model.instruction, "lang=python, mode=fast") {
		t.Fatalf("partial override not applied: %q", model.instruction)
	}
}

func TestBuildSequentialSubRecipeWithTools(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)
	toolRegistry := NewStaticToolRegistry()
	toolRegistry.Register("lint", &mockTool{name: "lint"})

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "pipeline",
		Model:       "m",
		Instruction: "coordinate",
		Execution:   ExecutionSequential,
		SubRecipes: []SubRecipeSpec{
			{Name: "linter", Instruction: "lint code", Tools: []string{"lint"}},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry), WithToolRegistry(toolRegistry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("go")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !slices.Contains(model.toolNames, "lint") {
		t.Fatalf("expected lint tool, got %v", model.toolNames)
	}
}

func TestBuildToolModeSubRecipeInheritsModel(t *testing.T) {
	model := &captureRequestModel{name: "m"}
	registry := NewRegistry()
	registry.Register("m", model)

	spec := &RecipeSpec{
		Version:     "1.0",
		Name:        "router",
		Model:       "m",
		Instruction: "route",
		Execution:   ExecutionTool,
		SubRecipes: []SubRecipeSpec{
			{Name: "worker", Instruction: "work", Description: "does work"},
		},
	}
	agent, err := Build(spec, WithModelRegistry(registry))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("go")); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !slices.Contains(model.toolNames, "worker") {
		t.Fatalf("expected worker tool, got %v", model.toolNames)
	}
}

func TestBuildFromYAMLBytesRoundTrip(t *testing.T) {
	yaml := `
version: "1.0"
name: roundtrip
model: gpt-4o
parameters:
  - name: x
    type: string
    required: required
instruction: "val={{.x}}"
`
	spec, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	agent, err := Build(spec, WithModelRegistry(newTestModelRegistry()), WithParams(map[string]any{"x": "42"}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if agent.Name() != "roundtrip" {
		t.Fatalf("unexpected name: %q", agent.Name())
	}
}


// Compile-time check that mockTool implements tools.Tool.
var _ tools.Tool = (*mockTool)(nil)
