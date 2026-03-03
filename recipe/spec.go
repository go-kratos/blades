package recipe

// ExecutionMode defines how sub-recipes are executed.
type ExecutionMode string

const (
	// ExecutionSequential runs sub-recipes one after another.
	ExecutionSequential ExecutionMode = "sequential"
	// ExecutionParallel runs sub-recipes concurrently.
	ExecutionParallel ExecutionMode = "parallel"
	// ExecutionTool wraps each sub-recipe as a tool for the parent agent.
	ExecutionTool ExecutionMode = "tool"
)

// ParameterType defines the type of a recipe parameter.
type ParameterType string

const (
	ParameterString  ParameterType = "string"
	ParameterNumber  ParameterType = "number"
	ParameterBoolean ParameterType = "boolean"
	ParameterSelect  ParameterType = "select"
)

// ParameterRequirement defines whether a parameter is required or optional.
type ParameterRequirement string

const (
	ParameterRequired ParameterRequirement = "required"
	ParameterOptional ParameterRequirement = "optional"
)

// RecipeSpec is the top-level declarative specification for a recipe.
// A recipe YAML file is parsed into this structure and then built into a blades.Agent.
type RecipeSpec struct {
	Version       string          `yaml:"version"`
	Name          string          `yaml:"name"`
	Description   string          `yaml:"description"`
	Model         string          `yaml:"model,omitempty"`
	Instruction   string          `yaml:"instruction"`
	Prompt        string          `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec `yaml:"parameters,omitempty"`
	SubRecipes    []SubRecipeSpec `yaml:"sub_recipes,omitempty"`
	Execution     ExecutionMode   `yaml:"execution,omitempty"`
	Tools         []string        `yaml:"tools,omitempty"`
	OutputKey     string          `yaml:"output_key,omitempty"`
	MaxIterations int             `yaml:"max_iterations,omitempty"`
}

// SubRecipeSpec defines a child agent within a recipe.
type SubRecipeSpec struct {
	Name          string          `yaml:"name"`
	Description   string          `yaml:"description,omitempty"`
	Model         string          `yaml:"model,omitempty"`
	Instruction   string          `yaml:"instruction"`
	Prompt        string          `yaml:"prompt,omitempty"`
	Parameters    []ParameterSpec `yaml:"parameters,omitempty"`
	Tools         []string        `yaml:"tools,omitempty"`
	OutputKey     string          `yaml:"output_key,omitempty"`
	MaxIterations int             `yaml:"max_iterations,omitempty"`
}

// ParameterSpec defines a configurable parameter for a recipe.
type ParameterSpec struct {
	Name        string               `yaml:"name"`
	Type        ParameterType        `yaml:"type"`
	Description string               `yaml:"description"`
	Default     any                  `yaml:"default,omitempty"`
	Required    ParameterRequirement `yaml:"required,omitempty"`
	Options     []string             `yaml:"options,omitempty"`
}
