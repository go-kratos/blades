package recipe

import (
	"fmt"
	"slices"
)

// Validate checks the RecipeSpec for consistency and required fields.
func Validate(spec *RecipeSpec) error {
	if spec == nil {
		return fmt.Errorf("recipe: spec is required")
	}
	if spec.Version == "" {
		return fmt.Errorf("recipe: version is required")
	}
	if spec.Name == "" {
		return fmt.Errorf("recipe: name is required")
	}
	// instruction is required except for sequential/parallel modes where
	// the flow agent has no LLM call and only orchestrates sub-agents.
	if spec.Instruction == "" && spec.Execution != ExecutionSequential && spec.Execution != ExecutionParallel {
		return fmt.Errorf("recipe: instruction is required")
	}
	if len(spec.SubRecipes) == 0 && spec.Model == "" {
		return fmt.Errorf("recipe: model is required when there are no sub_recipes")
	}
	if len(spec.SubRecipes) > 0 && spec.Execution == "" {
		return fmt.Errorf("recipe: execution mode is required when sub_recipes are defined")
	}
	if spec.Execution != "" && spec.Execution != ExecutionSequential &&
		spec.Execution != ExecutionParallel && spec.Execution != ExecutionTool {
		return fmt.Errorf("recipe: invalid execution mode %q (must be sequential, parallel, or tool)", spec.Execution)
	}
	// tool mode needs a parent model for the orchestrating LLM call.
	if spec.Execution == ExecutionTool && spec.Model == "" {
		return fmt.Errorf("recipe: model is required for tool execution mode")
	}
	if err := validateParameters(spec.Parameters); err != nil {
		return fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	subNames := make(map[string]bool, len(spec.SubRecipes))
	toolNames := make(map[string]bool, len(spec.Tools))
	for _, t := range spec.Tools {
		toolNames[t] = true
	}
	for i := range spec.SubRecipes {
		sub := &spec.SubRecipes[i]
		if err := validateSubRecipe(sub, i); err != nil {
			return fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		if subNames[sub.Name] {
			return fmt.Errorf("recipe %q: duplicate sub_recipe name %q", spec.Name, sub.Name)
		}
		subNames[sub.Name] = true
		if spec.Execution == ExecutionTool && toolNames[sub.Name] {
			return fmt.Errorf("recipe %q: sub_recipe %q conflicts with an external tool of the same name", spec.Name, sub.Name)
		}
		if spec.Execution == ExecutionTool && sub.OutputKey != "" {
			return fmt.Errorf("recipe %q: sub_recipe %q: output_key is not supported in tool mode", spec.Name, sub.Name)
		}
		// In sequential/parallel mode, if the parent has no model, each sub_recipe must specify its own.
		if (spec.Execution == ExecutionSequential || spec.Execution == ExecutionParallel) &&
			spec.Model == "" && sub.Model == "" {
			return fmt.Errorf("recipe %q: sub_recipe %q: model is required when parent has no model", spec.Name, sub.Name)
		}
	}
	return nil
}

func validateParameters(params []ParameterSpec) error {
	seen := make(map[string]bool, len(params))
	for _, p := range params {
		if p.Name == "" {
			return fmt.Errorf("parameter name is required")
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate parameter %q", p.Name)
		}
		seen[p.Name] = true
		if p.Type == "" {
			return fmt.Errorf("parameter %q: type is required", p.Name)
		}
		if p.Type != ParameterString && p.Type != ParameterNumber &&
			p.Type != ParameterBoolean && p.Type != ParameterSelect {
			return fmt.Errorf("parameter %q: invalid type %q", p.Name, p.Type)
		}
		if p.Required != "" && p.Required != ParameterRequired && p.Required != ParameterOptional {
			return fmt.Errorf("parameter %q: invalid required value %q", p.Name, p.Required)
		}
		if p.Type == ParameterSelect {
			if len(p.Options) == 0 {
				return fmt.Errorf("parameter %q: select type requires options", p.Name)
			}
			if p.Default != nil {
				def, ok := p.Default.(string)
				if !ok {
					return fmt.Errorf("parameter %q: default must be a string for select type", p.Name)
				}
				if !slices.Contains(p.Options, def) {
					return fmt.Errorf("parameter %q: default value %q is not in options %v", p.Name, def, p.Options)
				}
			}
		}
	}
	return nil
}

func validateSubRecipe(sub *SubRecipeSpec, index int) error {
	if sub.Name == "" {
		return fmt.Errorf("sub_recipe[%d]: name is required", index)
	}
	if sub.Instruction == "" {
		return fmt.Errorf("sub_recipe %q: instruction is required", sub.Name)
	}
	if err := validateParameters(sub.Parameters); err != nil {
		return fmt.Errorf("sub_recipe %q: %w", sub.Name, err)
	}
	return nil
}

// ValidateParams checks that provided parameter values satisfy the spec.
func ValidateParams(spec *RecipeSpec, params map[string]any) error {
	if spec == nil {
		return fmt.Errorf("recipe: spec is required")
	}
	return validateParamValues(fmt.Sprintf("recipe %q", spec.Name), spec.Parameters, params)
}

func validateParamValues(scope string, paramSpecs []ParameterSpec, params map[string]any) error {
	for _, p := range paramSpecs {
		val, ok := params[p.Name]
		if !ok && p.Default != nil {
			continue
		}
		if !ok && p.Required == ParameterRequired {
			return fmt.Errorf("%s: required parameter %q is missing", scope, p.Name)
		}
		if !ok {
			continue
		}
		if err := validateParamType(scope, p, val); err != nil {
			return err
		}
	}
	return nil
}

func validateParamType(scope string, p ParameterSpec, val any) error {
	switch p.Type {
	case ParameterString:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%s: parameter %q must be a string", scope, p.Name)
		}
	case ParameterNumber:
		switch val.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
		default:
			return fmt.Errorf("%s: parameter %q must be a number", scope, p.Name)
		}
	case ParameterBoolean:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s: parameter %q must be a boolean", scope, p.Name)
		}
	case ParameterSelect:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("%s: parameter %q must be a string for select type", scope, p.Name)
		}
		if !slices.Contains(p.Options, s) {
			return fmt.Errorf("%s: parameter %q value %q is not in options %v", scope, p.Name, s, p.Options)
		}
	}
	return nil
}
