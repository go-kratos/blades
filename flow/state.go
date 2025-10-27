package flow

import "maps"

// State represents the mutable data that flows through the graph.
// It is implemented as a map of string keys to arbitrary values.
// Handlers should treat State as immutable and always return a cloned instance.
type State map[string]any

// Clone performs a shallow copy using maps.Clone so callers can mutate without
// affecting the original map (nested references are shared intentionally).
func (s State) Clone() State {
	if s == nil {
		return State{}
	}
	return State(maps.Clone(map[string]any(s)))
}

// mergeStates clones the base state (if provided) and merges the updates in order.
func mergeStates(base State, updates ...State) State {
	merged := State{}
	if base != nil {
		merged = base.Clone()
	}
	for _, update := range updates {
		if update == nil {
			continue
		}
		for k, v := range update {
			if existing, ok := merged[k]; ok {
				merged[k] = mergeValues(existing, v)
				continue
			}
			merged[k] = v
		}
	}
	return merged
}

func mergeValues(existing, update any) any {
	if existing == nil {
		return update
	}
	switch ex := existing.(type) {
	case map[string]any:
		up, ok := update.(map[string]any)
		if !ok {
			return update
		}
		for k, v := range up {
			ex[k] = v
		}
		return ex
	case []any:
		switch up := update.(type) {
		case []any:
			return append(ex, up...)
		default:
			return update
		}
	case []string:
		if up, ok := update.([]string); ok {
			return append(ex, up...)
		}
		return update
	case []int:
		if up, ok := update.([]int); ok {
			return append(ex, up...)
		}
		return update
	default:
	}
	return update
}
