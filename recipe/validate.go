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
	if spec.Instruction == "" {
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
	if err := validateParameters(spec.Parameters); err != nil {
		return fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	for i, sub := range spec.SubRecipes {
		if err := validateSubRecipe(&sub, i); err != nil {
			return fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		if spec.Execution == ExecutionTool && sub.OutputKey != "" {
			return fmt.Errorf("recipe %q: sub_recipe %q: output_key is not supported in tool mode", spec.Name, sub.Name)
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
		if p.Type == ParameterSelect {
			if len(p.Options) == 0 {
				return fmt.Errorf("parameter %q: select type requires options", p.Name)
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
		if p.Type == ParameterSelect {
			s, ok := val.(string)
			if !ok {
				return fmt.Errorf("%s: parameter %q must be a string for select type", scope, p.Name)
			}
			if !slices.Contains(p.Options, s) {
				return fmt.Errorf("%s: parameter %q value %q is not in options %v", scope, p.Name, s, p.Options)
			}
		}
	}
	return nil
}
