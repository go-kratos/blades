package recipe

import (
	"fmt"
	"slices"
)

// Validate checks the AgentSpec for consistency and required fields.
func Validate(spec *AgentSpec) error {
	if spec == nil {
		return fmt.Errorf("recipe: spec is required")
	}
	if spec.Version == "" {
		return fmt.Errorf("recipe: version is required")
	}
	if spec.Name == "" {
		return fmt.Errorf("recipe: name is required")
	}
	// instruction is required except for sequential/parallel/loop modes where
	// the flow agent has no LLM call and only orchestrates sub-agents.
	if spec.Instruction == "" && spec.Execution != ExecutionSequential &&
		spec.Execution != ExecutionParallel && spec.Execution != ExecutionLoop {
		return fmt.Errorf("recipe: instruction is required")
	}
	if len(spec.SubAgents) == 0 && spec.Model == "" {
		return fmt.Errorf("recipe: model is required when there are no sub_agents")
	}
	if len(spec.SubAgents) > 0 && spec.Execution == "" {
		return fmt.Errorf("recipe: execution mode is required when sub_agents are defined")
	}
	if spec.Execution != "" && spec.Execution != ExecutionSequential &&
		spec.Execution != ExecutionParallel && spec.Execution != ExecutionTool &&
		spec.Execution != ExecutionLoop {
		return fmt.Errorf("recipe: invalid execution mode %q (must be sequential, parallel, tool, or loop)", spec.Execution)
	}
	// tool mode needs a parent model for the orchestrating LLM call.
	if spec.Execution == ExecutionTool && spec.Model == "" {
		return fmt.Errorf("recipe: model is required for tool execution mode")
	}
	// sequential/parallel modes use flow agents that don't support these fields.
	if spec.Execution == ExecutionSequential || spec.Execution == ExecutionParallel {
		if spec.OutputKey != "" {
			return fmt.Errorf("recipe %q: output_key is not supported in %s mode", spec.Name, spec.Execution)
		}
		if spec.MaxIterations > 0 {
			return fmt.Errorf("recipe %q: max_iterations is not supported in %s mode", spec.Name, spec.Execution)
		}
	}
	// loop mode: output_key is not supported since LoopAgent makes no LLM call.
	if spec.Execution == ExecutionLoop && spec.OutputKey != "" {
		return fmt.Errorf("recipe %q: output_key is not supported in loop mode", spec.Name)
	}
	if err := validateParameters(spec.Parameters); err != nil {
		return fmt.Errorf("recipe %q: %w", spec.Name, err)
	}
	if err := validateContextSpec(spec.Context); err != nil {
		return fmt.Errorf("recipe %q: context: %w", spec.Name, err)
	}
	if err := validateMiddlewares(fmt.Sprintf("recipe %q", spec.Name), spec.Middlewares); err != nil {
		return err
	}
	toolNames, err := validateToolNames(fmt.Sprintf("recipe %q", spec.Name), spec.Tools)
	if err != nil {
		return err
	}
	subNames := make(map[string]bool, len(spec.SubAgents))
	for i := range spec.SubAgents {
		sub := &spec.SubAgents[i]
		if err := validateSubAgent(sub, i); err != nil {
			return fmt.Errorf("recipe %q: %w", spec.Name, err)
		}
		if subNames[sub.Name] {
			return fmt.Errorf("recipe %q: duplicate sub_agent name %q", spec.Name, sub.Name)
		}
		subNames[sub.Name] = true
		if spec.Execution == ExecutionTool && toolNames[sub.Name] {
			return fmt.Errorf("recipe %q: sub_agent %q conflicts with an external tool of the same name", spec.Name, sub.Name)
		}
		if spec.Execution == ExecutionTool && sub.OutputKey != "" {
			return fmt.Errorf("recipe %q: sub_agent %q: output_key is not supported in tool mode", spec.Name, sub.Name)
		}
		// In sequential/parallel/loop mode, if the parent has no model, each sub_agent must specify its own.
		if (spec.Execution == ExecutionSequential || spec.Execution == ExecutionParallel || spec.Execution == ExecutionLoop) &&
			spec.Model == "" && sub.Model == "" {
			return fmt.Errorf("recipe %q: sub_agent %q: model is required when parent has no model", spec.Name, sub.Name)
		}
	}
	return nil
}

func validateMiddlewares(scope string, specs []MiddlewareSpec) error {
	seen := make(map[string]bool, len(specs))
	for i, mw := range specs {
		if mw.Name == "" {
			return fmt.Errorf("%s: middleware[%d]: name is required", scope, i)
		}
		if seen[mw.Name] {
			return fmt.Errorf("%s: duplicate middleware name %q", scope, mw.Name)
		}
		seen[mw.Name] = true
	}
	return nil
}

func validateContextSpec(spec *ContextSpec) error {
	if spec == nil {
		return nil
	}
	switch spec.Strategy {
	case ContextStrategySummarize:
		// model is optional; falls back to the agent's model at build time
	case ContextStrategyWindow:
		// max_tokens and/or max_messages are optional but at least one is expected
	case "":
		return fmt.Errorf("strategy is required")
	default:
		return fmt.Errorf("unknown strategy %q (must be %q or %q)", spec.Strategy, ContextStrategySummarize, ContextStrategyWindow)
	}
	if spec.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be >= 0")
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
		if p.Type == ParameterSelect && len(p.Options) == 0 {
			return fmt.Errorf("parameter %q: select type requires options", p.Name)
		}
		if p.Default != nil {
			if err := validateParamType("default value", p, p.Default); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSubAgent(sub *SubAgentSpec, index int) error {
	if sub.Name == "" {
		return fmt.Errorf("sub_agent[%d]: name is required", index)
	}
	if sub.Instruction == "" {
		return fmt.Errorf("sub_agent %q: instruction is required", sub.Name)
	}
	if err := validateParameters(sub.Parameters); err != nil {
		return fmt.Errorf("sub_agent %q: %w", sub.Name, err)
	}
	if _, err := validateToolNames(fmt.Sprintf("sub_agent %q", sub.Name), sub.Tools); err != nil {
		return err
	}
	if err := validateContextSpec(sub.Context); err != nil {
		return fmt.Errorf("sub_agent %q: context: %w", sub.Name, err)
	}
	if err := validateMiddlewares(fmt.Sprintf("sub_agent %q", sub.Name), sub.Middlewares); err != nil {
		return err
	}
	return nil
}

func validateToolNames(scope string, toolNames []string) (map[string]bool, error) {
	seen := make(map[string]bool, len(toolNames))
	for _, t := range toolNames {
		if t == "" {
			return nil, fmt.Errorf("%s: tool name must be non-empty", scope)
		}
		if seen[t] {
			return nil, fmt.Errorf("%s: duplicate tool name %q", scope, t)
		}
		seen[t] = true
	}
	return seen, nil
}

// ValidateParams checks that provided parameter values satisfy the spec.
func ValidateParams(spec *AgentSpec, params map[string]any) error {
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
